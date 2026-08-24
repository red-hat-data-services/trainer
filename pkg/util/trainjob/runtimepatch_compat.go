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
	"k8s.io/apimachinery/pkg/util/validation/field"

	trainer "github.com/kubeflow/trainer/v2/pkg/apis/trainer/v1alpha1"
)

// EffectivePodTemplateOverrides returns spec.podTemplateOverrides followed by the
// overrides derived from spec.runtimePatches, so patch-managers win over user values
// (matching Kueue 0.16's append-last ordering).
//
// STUB: translation from runtimePatches is not implemented yet. This intentionally
// ignores spec.RuntimePatches so callers compile against the intended final API while
// tests exercising real translation remain red for the right reason.
func EffectivePodTemplateOverrides(trainJob *trainer.TrainJob) ([]trainer.PodTemplateOverride, field.ErrorList) {
	return trainJob.Spec.PodTemplateOverrides, nil
}
