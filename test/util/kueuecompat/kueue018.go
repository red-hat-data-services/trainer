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

// Copied (with adaptations noted below) from sigs.k8s.io/kueue @ release-0.18:
//
//	pkg/controller/jobs/trainjob/trainjob_webhook.go:91-102     -> the runtimePatches seeding
//	                                                                 logic inside TrainJobWebhook.Default,
//	                                                                 extracted here as SeedRuntimePatch
//	pkg/controller/jobs/trainjob/trainjob_controller.go:243-290 -> RunWithPodSetsInfo
//	pkg/controller/jobs/trainjob/trainjob_controller.go:315-331 -> RestorePodSetsInfo, getKueueRuntimePatch
//
// Do not refactor, tidy, or improve this code — its value is that it is byte-identical
// (module import path and the two deviations below aside) to what Kueue executes.
//
// Deviations from upstream, both required to compile standalone as a test helper rather
// than as part of a full jobframework.GenericJob implementation:
//   - The trainer API import path is retyped to this module's
//     "github.com/kubeflow/trainer/v2/pkg/apis/trainer/v1alpha1".
//   - podset.PodSetInfo is replaced by the local kueuecompat.PodSetInfo mirror
//     (see podset.go for why the real sigs.k8s.io/kueue dependency was reverted).
//   - RunWithPodSetsInfo here takes the already-known replicated job names
//     ([]string) instead of deriving them from the child JobSet via
//     getChildJobSet/jobframework machinery, which pulls in the full Kueue
//     jobframework.GenericJob/reconciler stack that is out of scope for this
//     compat shim. The upstream length-check
//     (podset.BadPodSetsInfoLenError) and the two-phase Update+Unsuspend
//     behavior are preserved.
package kueuecompat

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	trainer "github.com/kubeflow/trainer/v2/pkg/apis/trainer/v1alpha1"
)

// RuntimePatchManagerName is Kueue's manager identity for the runtimePatches
// entry it owns.
//
// Copied from trainjob_controller.go:76 (`const runtimePatchManagerName`).
const RuntimePatchManagerName = "kueue.x-k8s.io/manager"

// SeedRuntimePatch appends the empty, Kueue-owned runtimePatches entry that
// Kueue 0.18's mutating webhook writes on every TrainJob create.
//
// Copied from trainjob_webhook.go:91-102 (inside TrainJobWebhook.Default).
func SeedRuntimePatch(trainJob *trainer.TrainJob) {
	runtimePatch := trainer.RuntimePatch{
		Manager: RuntimePatchManagerName,
		TrainingRuntimeSpec: &trainer.TrainingRuntimeSpecPatch{
			Template: &trainer.JobSetTemplatePatch{
				Spec: &trainer.JobSetSpecPatch{},
			},
		},
	}
	trainJob.Spec.RuntimePatches = append(trainJob.Spec.RuntimePatches, runtimePatch)
}

// RunWithPodSetsInfo fills in the Kueue-owned runtimePatches entry's
// replicatedJobs with per-podset scheduling info, updates the TrainJob while
// suspended, then unsuspends it.
//
// Copied from trainjob_controller.go:243-290 (TrainJob.RunWithPodSetsInfo),
// with replicatedJobNames passed in directly instead of being derived from
// the child JobSet (see package doc comment).
func RunWithPodSetsInfo(ctx context.Context, c client.Client, trainJob *trainer.TrainJob, replicatedJobNames []string, podSetsInfo []PodSetInfo) error {
	if len(podSetsInfo) != len(replicatedJobNames) {
		return fmt.Errorf("invalid podSetsInfo, got %d, want %d", len(podSetsInfo), len(replicatedJobNames))
	}

	var replicatedJobPatches []trainer.ReplicatedJobPatch
	for _, info := range podSetsInfo {
		replicatedJobPatches = append(replicatedJobPatches,
			trainer.ReplicatedJobPatch{
				Name: info.Name,
				Template: &trainer.JobTemplatePatch{
					Spec: &trainer.JobSpecPatch{
						Template: &trainer.PodTemplatePatch{
							Metadata: &metav1.ObjectMeta{
								Annotations: info.Annotations,
								Labels:      info.Labels,
							},
							Spec: &trainer.PodSpecPatch{
								NodeSelector:    info.NodeSelector,
								Tolerations:     info.Tolerations,
								SchedulingGates: info.SchedulingGates,
							},
						},
					},
				},
			},
		)
	}

	kueueRuntimePatch := getKueueRuntimePatch(trainJob)
	if kueueRuntimePatch == nil {
		return errors.New("kueue runtime patch not found")
	}
	kueueRuntimePatch.TrainingRuntimeSpec.Template.Spec.ReplicatedJobs = replicatedJobPatches
	// Update the runtimePatches while the job is suspended, since is a requirement from the trainjob admission webhook
	if err := c.Update(ctx, trainJob); err != nil {
		return err
	}

	suspend := false
	trainJob.Spec.Suspend = &suspend
	return nil
}

// RestorePodSetsInfo clears the Kueue-owned runtimePatches entry's
// replicatedJobs while leaving the manager entry itself in place.
//
// Copied from trainjob_controller.go:315-322 (TrainJob.RestorePodSetsInfo).
func RestorePodSetsInfo(trainJob *trainer.TrainJob, _ []PodSetInfo) bool {
	kueueRuntimePatch := getKueueRuntimePatch(trainJob)
	if kueueRuntimePatch == nil {
		return false
	}
	kueueRuntimePatch.TrainingRuntimeSpec.Template.Spec.ReplicatedJobs = nil
	return true
}

// getKueueRuntimePatch returns the runtimePatches entry owned by Kueue, or
// nil if it is absent.
//
// Copied from trainjob_controller.go:324-331.
func getKueueRuntimePatch(trainJob *trainer.TrainJob) *trainer.RuntimePatch {
	for i := range trainJob.Spec.RuntimePatches {
		if trainJob.Spec.RuntimePatches[i].Manager == RuntimePatchManagerName {
			return &trainJob.Spec.RuntimePatches[i]
		}
	}
	return nil
}
