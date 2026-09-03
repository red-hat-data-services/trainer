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

// Copied (with adaptations noted below) from sigs.k8s.io/kueue @ release-0.16:
//
//	pkg/controller/jobs/trainjob/trainjob_controller.go:267-319 -> RunWithPodSetsInfo
//	pkg/controller/jobs/trainjob/trainjob_controller.go:344-356 -> RestorePodSetsInfo
//	pkg/constants/constants.go:51                               -> PodSetLabel
//
// Do not refactor, tidy, or improve this code — its value is that it is byte-identical
// (module import path and the deviations below aside) to what Kueue 0.16 executes,
// holding the legacy podTemplateOverrides-writing path to the same standard as the
// kueue018.go shim.
//
// Deviations from upstream, both required to compile standalone as a test helper:
//   - The trainer API import path is retyped to this module's
//     "github.com/kubeflow/trainer/v2/pkg/apis/trainer/v1alpha1".
//   - podset.PodSetInfo is replaced by the local kueuecompat.PodSetInfo mirror.
//   - RunWithPodSetsInfo here takes the already-known replicated job names
//     ([]string) instead of deriving them from the child JobSet, for the same
//     reason as kueue018.go's shim.
package kueuecompat

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	trainer "github.com/kubeflow/trainer/v2/pkg/apis/trainer/v1alpha1"
)

// PodSetLabelKueue016 is the label Kueue 0.16 stamps on the Metadata of each
// podTemplateOverride it writes, so it can find (and later strip) its own
// entries idempotently.
//
// Copied from pkg/constants/constants.go:51 (`constants.PodSetLabel`).
const PodSetLabelKueue016 = "kueue.x-k8s.io/podset"

// RunWithPodSetsInfoKueue016 writes one podTemplateOverride per podset,
// filtering out any pre-existing Kueue-authored overrides first so retries
// are idempotent, then updates the TrainJob while suspended and unsuspends.
//
// Copied from trainjob_controller.go:267-319 (TrainJob.RunWithPodSetsInfo, Kueue 0.16).
func RunWithPodSetsInfoKueue016(ctx context.Context, c client.Client, trainJob *trainer.TrainJob, replicatedJobNames []string, podSetsInfo []PodSetInfo) error {
	if len(podSetsInfo) != len(replicatedJobNames) {
		return fmt.Errorf("invalid podSetsInfo, got %d, want %d", len(podSetsInfo), len(replicatedJobNames))
	}

	if trainJob.Spec.PodTemplateOverrides == nil {
		trainJob.Spec.PodTemplateOverrides = []trainer.PodTemplateOverride{}
	}
	if trainJob.Annotations == nil {
		trainJob.Annotations = map[string]string{}
	}
	// Filter out the existing overrides that were added by Kueue
	// (identified by the presence of the PodSetLabel).
	// This makes the function idempotent, preventing duplicate overrides
	// if the update operation is retried.
	var userOverrides []trainer.PodTemplateOverride
	for _, o := range trainJob.Spec.PodTemplateOverrides {
		if o.Metadata == nil || o.Metadata.Labels == nil || o.Metadata.Labels[PodSetLabelKueue016] == "" {
			userOverrides = append(userOverrides, o)
		}
	}
	trainJob.Spec.PodTemplateOverrides = userOverrides
	for _, info := range podSetsInfo {
		// The trainjob controller merges each podSpecOverride sequentially, so any existing user provided override will be processed first
		trainJob.Spec.PodTemplateOverrides = append(trainJob.Spec.PodTemplateOverrides, trainer.PodTemplateOverride{
			TargetJobs: []trainer.PodTemplateOverrideTargetJob{
				{Name: info.Name},
			},
			Metadata: &metav1.ObjectMeta{
				Annotations: info.Annotations,
				Labels:      info.Labels,
			},
			Spec: &trainer.PodTemplateSpecOverride{
				NodeSelector:    info.NodeSelector,
				Tolerations:     info.Tolerations,
				SchedulingGates: info.SchedulingGates,
			},
		})
	}
	// Update the podSpecOverrides while the job is suspended, since is a requirement from the trainjob admission webhook
	if err := c.Update(ctx, trainJob); err != nil {
		return err
	}

	suspend := false
	trainJob.Spec.Suspend = &suspend
	return nil
}

// RestorePodSetsInfoKueue016 removes every podTemplateOverride from the point
// the first Kueue-authored one (identified by PodSetLabelKueue016) appears
// onward, leaving user-supplied overrides that preceded it intact.
//
// Copied from trainjob_controller.go:344-356 (TrainJob.RestorePodSetsInfo, Kueue 0.16).
func RestorePodSetsInfoKueue016(trainJob *trainer.TrainJob, _ []PodSetInfo) bool {
	for i, o := range trainJob.Spec.PodTemplateOverrides {
		if o.Metadata != nil {
			if _, exists := o.Metadata.Labels[PodSetLabelKueue016]; exists {
				trainJob.Spec.PodTemplateOverrides = trainJob.Spec.PodTemplateOverrides[:i]
				break
			}
		}
	}

	return true
}
