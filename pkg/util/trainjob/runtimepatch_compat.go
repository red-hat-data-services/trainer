/*
Copyright 2025 The Kubeflow Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package trainjob

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"

	trainer "github.com/kubeflow/trainer/v2/pkg/apis/trainer/v1alpha1"
)

// PodTemplateOverrideWithOrigin pairs an effective PodTemplateOverride with the field
// path it originated from (spec.podTemplateOverrides[j] for user-supplied overrides, or
// spec.runtimePatches[i]... for overrides derived from a RuntimePatch), so validators can
// report errors against the field the value actually came from.
type PodTemplateOverrideWithOrigin struct {
	Override trainer.PodTemplateOverride
	Path     *field.Path
}

// EffectivePodTemplateOverrides returns spec.podTemplateOverrides followed by the
// overrides derived from spec.runtimePatches, so patch-managers win over user values
// (matching Kueue 0.16's append-last ordering).
//
// Translation is mechanical: each RuntimePatch's replicatedJobs[] entries become one
// PodTemplateOverride per replicated job, in slice order. Fields with no 2.1
// PodTemplateOverride equivalent (JobSet-level metadata, Job-level metadata, and
// SecurityContext on the pod or container patches) are rejected with a field.Error
// rather than silently dropped, since Kueue never sets them and their presence implies
// a hand-written patch the translator cannot honor.
func EffectivePodTemplateOverrides(trainJob *trainer.TrainJob) ([]trainer.PodTemplateOverride, field.ErrorList) {
	withOrigin, errs := EffectivePodTemplateOverridesWithOrigin(trainJob)
	if len(errs) != 0 {
		return nil, errs
	}
	if len(withOrigin) == 0 {
		return nil, nil
	}
	overrides := make([]trainer.PodTemplateOverride, 0, len(withOrigin))
	for _, o := range withOrigin {
		overrides = append(overrides, o.Override)
	}
	return overrides, nil
}

// EffectivePodTemplateOverridesWithOrigin is EffectivePodTemplateOverrides but also
// returns, for each override, the field.Path it originated from.
func EffectivePodTemplateOverridesWithOrigin(trainJob *trainer.TrainJob) ([]PodTemplateOverrideWithOrigin, field.ErrorList) {
	var errs field.ErrorList
	var overrides []PodTemplateOverrideWithOrigin
	userBasePath := field.NewPath("spec", "podTemplateOverrides")
	for j, o := range trainJob.Spec.PodTemplateOverrides {
		overrides = append(overrides, PodTemplateOverrideWithOrigin{Override: o, Path: userBasePath.Index(j)})
	}

	for i, rp := range trainJob.Spec.RuntimePatches {
		patchPath := field.NewPath("spec").Child("runtimePatches").Index(i)
		if rp.TrainingRuntimeSpec == nil || rp.TrainingRuntimeSpec.Template == nil {
			continue
		}
		jobSetTemplate := rp.TrainingRuntimeSpec.Template
		templatePath := patchPath.Child("trainingRuntimeSpec", "template")
		if jobSetTemplate.Metadata != nil {
			errs = append(errs, field.Invalid(templatePath.Child("metadata"), jobSetTemplate.Metadata,
				"JobSet-level metadata patches have no podTemplateOverrides equivalent and cannot be translated"))
		}
		if jobSetTemplate.Spec == nil {
			continue
		}
		for _, rj := range jobSetTemplate.Spec.ReplicatedJobs {
			rjPath := templatePath.Child("spec", "replicatedJobs").Key(rj.Name)
			if rj.Template == nil {
				continue
			}
			if rj.Template.Metadata != nil {
				errs = append(errs, field.Invalid(rjPath.Child("template", "metadata"), rj.Template.Metadata,
					"Job-level metadata patches have no podTemplateOverrides equivalent and cannot be translated"))
			}
			if rj.Template.Spec == nil || rj.Template.Spec.Template == nil {
				continue
			}
			podTemplatePatch := rj.Template.Spec.Template
			override := trainer.PodTemplateOverride{
				TargetJobs: []trainer.PodTemplateOverrideTargetJob{{Name: rj.Name}},
				Metadata:   podTemplatePatch.Metadata,
			}
			if podTemplatePatch.Spec != nil {
				specOverride, specErrs := convertPodSpecPatch(podTemplatePatch.Spec, rjPath.Child("template", "spec", "template", "spec"))
				errs = append(errs, specErrs...)
				override.Spec = specOverride
			}
			overrides = append(overrides, PodTemplateOverrideWithOrigin{Override: override, Path: rjPath})
		}
	}

	if len(errs) != 0 {
		return nil, errs
	}
	return overrides, nil
}

func convertPodSpecPatch(spec *trainer.PodSpecPatch, path *field.Path) (*trainer.PodTemplateSpecOverride, field.ErrorList) {
	var errs field.ErrorList
	if spec.SecurityContext != nil {
		errs = append(errs, field.Invalid(path.Child("securityContext"), spec.SecurityContext,
			"Pod securityContext patches have no podTemplateOverrides equivalent and cannot be translated"))
	}

	out := &trainer.PodTemplateSpecOverride{
		ServiceAccountName: spec.ServiceAccountName,
		NodeSelector:       spec.NodeSelector,
		Affinity:           spec.Affinity,
		Tolerations:        spec.Tolerations,
		Volumes:            spec.Volumes,
		SchedulingGates:    spec.SchedulingGates,
		ImagePullSecrets:   spec.ImagePullSecrets,
	}

	if len(spec.InitContainers) > 0 {
		initContainers, containerErrs := convertContainerPatches(spec.InitContainers, path.Child("initContainers"))
		errs = append(errs, containerErrs...)
		out.InitContainers = initContainers
	}
	if len(spec.Containers) > 0 {
		containers, containerErrs := convertContainerPatches(spec.Containers, path.Child("containers"))
		errs = append(errs, containerErrs...)
		out.Containers = containers
	}

	if len(errs) != 0 {
		return nil, errs
	}
	return out, nil
}

func convertContainerPatches(containers []trainer.ContainerPatch, path *field.Path) ([]trainer.ContainerOverride, field.ErrorList) {
	var errs field.ErrorList
	out := make([]trainer.ContainerOverride, 0, len(containers))
	for i, c := range containers {
		cPath := path.Index(i)
		if c.SecurityContext != nil {
			errs = append(errs, field.Invalid(cPath.Child("securityContext"), c.SecurityContext,
				fmt.Sprintf("container %q securityContext patches have no podTemplateOverrides equivalent and cannot be translated", c.Name)))
			continue
		}
		out = append(out, trainer.ContainerOverride{
			Name:         c.Name,
			Env:          c.Env,
			VolumeMounts: c.VolumeMounts,
		})
	}
	return out, errs
}
