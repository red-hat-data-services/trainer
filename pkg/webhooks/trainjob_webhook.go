/*
Copyright 2024 The Kubeflow Authors.

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

package webhooks

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	trainer "github.com/kubeflow/trainer/v2/pkg/apis/trainer/v1alpha1"
	"github.com/kubeflow/trainer/v2/pkg/runtime"
	trainjobutil "github.com/kubeflow/trainer/v2/pkg/util/trainjob"
)

type TrainJobWebhook struct {
	runtimes map[string]runtime.Runtime
}

func setupWebhookForTrainJob(mgr ctrl.Manager, run map[string]runtime.Runtime) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&trainer.TrainJob{}).
		WithValidator(&TrainJobWebhook{runtimes: run}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-trainer-kubeflow-org-v1alpha1-trainjob,mutating=false,failurePolicy=fail,sideEffects=None,groups=trainer.kubeflow.org,resources=trainjobs,verbs=create;update,versions=v1alpha1,name=validator.trainjob.trainer.kubeflow.org,admissionReviewVersions=v1

var _ webhook.CustomValidator = (*TrainJobWebhook)(nil)

func (w *TrainJobWebhook) ValidateCreate(ctx context.Context, obj apiruntime.Object) (admission.Warnings, error) {
	trainJob := obj.(*trainer.TrainJob)
	log := ctrl.LoggerFrom(ctx).WithName("trainJob-webhook")
	log.V(5).Info("Validating create", "TrainJob", klog.KObj(trainJob))

	if errs := validatePodTemplateOverridesSecurity(trainJob); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	if errs := validateKueueManagedNumProcPerNode(trainJob); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}

	runtimeRefGK := runtime.RuntimeRefToRuntimeRegistryKey(trainJob.Spec.RuntimeRef)
	runtime, ok := w.runtimes[runtimeRefGK]
	if !ok {
		return nil, fmt.Errorf("unsupported runtime: %s", runtimeRefGK)
	}
	warnings, errors := runtime.ValidateObjects(ctx, nil, trainJob)
	return warnings, errors.ToAggregate()
}

func (w *TrainJobWebhook) ValidateUpdate(ctx context.Context, oldObj apiruntime.Object, newObj apiruntime.Object) (admission.Warnings, error) {
	oldTrainJob := oldObj.(*trainer.TrainJob)
	newTrainJob := newObj.(*trainer.TrainJob)
	log := ctrl.LoggerFrom(ctx).WithName("trainJob-webhook")
	log.V(5).Info("Validating update", "TrainJob", klog.KObj(newTrainJob))

	if errs := validatePodTemplateOverridesSecurity(newTrainJob); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	if errs := validateKueueManagedNumProcPerNode(newTrainJob); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}

	runtimeRefGK := runtime.RuntimeRefToRuntimeRegistryKey(newTrainJob.Spec.RuntimeRef)
	runtime, ok := w.runtimes[runtimeRefGK]
	if !ok {
		return nil, fmt.Errorf("unsupported runtime: %s", runtimeRefGK)
	}
	warnings, errors := runtime.ValidateObjects(ctx, oldTrainJob, newTrainJob)
	return warnings, errors.ToAggregate()
}

func (w *TrainJobWebhook) ValidateDelete(context.Context, apiruntime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validatePodTemplateOverridesSecurity(trainJob *trainer.TrainJob) field.ErrorList {
	var allErrs field.ErrorList

	effectiveOverrides, errs := trainjobutil.EffectivePodTemplateOverridesWithOrigin(trainJob)
	if len(errs) != 0 {
		return errs
	}

	for _, entry := range effectiveOverrides {
		override := entry.Override
		if override.Spec == nil {
			continue
		}
		specPath := entry.Path.Child("spec")

		for j, vol := range override.Spec.Volumes {
			if vol.HostPath != nil {
				allErrs = append(allErrs, field.Forbidden(
					specPath.Child("volumes").Index(j).Child("hostPath"),
					"hostPath volumes are not allowed: they enable host filesystem access",
				))
			}
		}

		for j, tol := range override.Spec.Tolerations {
			if isControlPlaneToleration(tol) {
				allErrs = append(allErrs, field.Forbidden(
					specPath.Child("tolerations").Index(j),
					"tolerations targeting control-plane or master nodes are not allowed: they bypass node isolation",
				))
			}
		}
	}
	return allErrs
}

// kueueQueueNameLabel marks a TrainJob as managed by Kueue. Kueue 0.18 decodes the TrainJob
// into kubeflow/trainer v2.2 structs, where numProcPerNode is *int32; a string value fails
// that typed decode and the job is silently never admitted, so we reject it up front instead.
const kueueQueueNameLabel = "kueue.x-k8s.io/queue-name"

func validateKueueManagedNumProcPerNode(trainJob *trainer.TrainJob) field.ErrorList {
	var allErrs field.ErrorList

	if _, ok := trainJob.Labels[kueueQueueNameLabel]; !ok {
		return allErrs
	}
	if trainJob.Spec.Trainer == nil {
		return allErrs
	}
	numProcPerNode := trainJob.Spec.Trainer.NumProcPerNode
	if numProcPerNode != nil && numProcPerNode.Type == intstr.String {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("spec", "trainer", "numProcPerNode"),
			"string values are not supported when TrainJob is managed by Kueue "+
				"(the "+kueueQueueNameLabel+" label is set); use a numeric value or omit the field",
		))
	}
	return allErrs
}

func isControlPlaneToleration(tol corev1.Toleration) bool {
	dangerousKeys := []string{
		"node-role.kubernetes.io/control-plane",
		"node-role.kubernetes.io/master",
		"node-role.kubernetes.io/infra",
	}
	for _, key := range dangerousKeys {
		if tol.Key == key {
			return true
		}
	}
	if tol.Operator == corev1.TolerationOpExists && tol.Key == "" {
		return true
	}
	return false
}
