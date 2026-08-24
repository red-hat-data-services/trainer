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
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	trainer "github.com/kubeflow/trainer/v2/pkg/apis/trainer/v1alpha1"
	testingutil "github.com/kubeflow/trainer/v2/pkg/util/testing"
)

// TestEffectivePodTemplateOverrides is a red-only spec (see
// EffectivePodTemplateOverrides's STUB doc comment): it asserts the behavior
// the real runtimePatches->podTemplateOverrides translation must produce, not
// what the current stub returns. Every case is expected to fail until the
// translation lands.
func TestEffectivePodTemplateOverrides(t *testing.T) {
	kueueManager := "kueue.x-k8s.io/manager"

	cases := map[string]struct {
		trainJob *trainer.TrainJob
		want     []trainer.PodTemplateOverride
		wantErrs bool
	}{
		"empty webhook-seeded patch slot produces no overrides": {
			trainJob: testingutil.MakeTrainJobWrapper("ns", "job").
				RuntimePatches([]trainer.RuntimePatch{
					{
						Manager: kueueManager,
						TrainingRuntimeSpec: &trainer.TrainingRuntimeSpecPatch{
							Template: &trainer.JobSetTemplatePatch{
								Spec: &trainer.JobSetSpecPatch{},
							},
						},
					},
				}).
				Obj(),
			want: nil,
		},
		"filled single-podset patch translates to a PodTemplateOverride": {
			trainJob: testingutil.MakeTrainJobWrapper("ns", "job").
				RuntimePatches([]trainer.RuntimePatch{
					{
						Manager: kueueManager,
						TrainingRuntimeSpec: &trainer.TrainingRuntimeSpecPatch{
							Template: &trainer.JobSetTemplatePatch{
								Spec: &trainer.JobSetSpecPatch{
									ReplicatedJobs: []trainer.ReplicatedJobPatch{
										{
											Name: "trainer-node",
											Template: &trainer.JobTemplatePatch{
												Spec: &trainer.JobSpecPatch{
													Template: &trainer.PodTemplatePatch{
														Metadata: &metav1.ObjectMeta{
															Labels: map[string]string{"kueue.x-k8s.io/podset": "trainer-node"},
														},
														Spec: &trainer.PodSpecPatch{
															NodeSelector: map[string]string{"disktype": "ssd"},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				}).
				Obj(),
			want: []trainer.PodTemplateOverride{
				{
					TargetJobs: []trainer.PodTemplateOverrideTargetJob{{Name: "trainer-node"}},
					Metadata: &metav1.ObjectMeta{
						Labels: map[string]string{"kueue.x-k8s.io/podset": "trainer-node"},
					},
					Spec: &trainer.PodTemplateSpecOverride{
						NodeSelector: map[string]string{"disktype": "ssd"},
					},
				},
			},
		},
		"multi-podset patch translates to one PodTemplateOverride per replicatedJob": {
			trainJob: testingutil.MakeTrainJobWrapper("ns", "job").
				RuntimePatches([]trainer.RuntimePatch{
					{
						Manager: kueueManager,
						TrainingRuntimeSpec: &trainer.TrainingRuntimeSpecPatch{
							Template: &trainer.JobSetTemplatePatch{
								Spec: &trainer.JobSetSpecPatch{
									ReplicatedJobs: []trainer.ReplicatedJobPatch{
										{
											Name: "dataset-initializer",
											Template: &trainer.JobTemplatePatch{
												Spec: &trainer.JobSpecPatch{
													Template: &trainer.PodTemplatePatch{
														Spec: &trainer.PodSpecPatch{NodeSelector: map[string]string{"a": "1"}},
													},
												},
											},
										},
										{
											Name: "trainer-node",
											Template: &trainer.JobTemplatePatch{
												Spec: &trainer.JobSpecPatch{
													Template: &trainer.PodTemplatePatch{
														Spec: &trainer.PodSpecPatch{NodeSelector: map[string]string{"b": "2"}},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				}).
				Obj(),
			want: []trainer.PodTemplateOverride{
				{
					TargetJobs: []trainer.PodTemplateOverrideTargetJob{{Name: "dataset-initializer"}},
					Spec:       &trainer.PodTemplateSpecOverride{NodeSelector: map[string]string{"a": "1"}},
				},
				{
					TargetJobs: []trainer.PodTemplateOverrideTargetJob{{Name: "trainer-node"}},
					Spec:       &trainer.PodTemplateSpecOverride{NodeSelector: map[string]string{"b": "2"}},
				},
			},
		},
		"requeue state: replicatedJobs nil-ed out but manager entry remains produces no overrides": {
			trainJob: testingutil.MakeTrainJobWrapper("ns", "job").
				RuntimePatches([]trainer.RuntimePatch{
					{
						Manager: kueueManager,
						TrainingRuntimeSpec: &trainer.TrainingRuntimeSpecPatch{
							Template: &trainer.JobSetTemplatePatch{
								Spec: &trainer.JobSetSpecPatch{
									ReplicatedJobs: nil,
								},
							},
						},
					},
				}).
				Obj(),
			want: nil,
		},
		"ordering: derived overrides come after user-supplied ones": {
			trainJob: testingutil.MakeTrainJobWrapper("ns", "job").
				PodTemplateOverrides([]trainer.PodTemplateOverride{
					{
						TargetJobs: []trainer.PodTemplateOverrideTargetJob{{Name: "trainer-node"}},
						Spec:       &trainer.PodTemplateSpecOverride{ServiceAccountName: ptrTo("user-sa")},
					},
				}).
				RuntimePatches([]trainer.RuntimePatch{
					{
						Manager: kueueManager,
						TrainingRuntimeSpec: &trainer.TrainingRuntimeSpecPatch{
							Template: &trainer.JobSetTemplatePatch{
								Spec: &trainer.JobSetSpecPatch{
									ReplicatedJobs: []trainer.ReplicatedJobPatch{
										{
											Name: "trainer-node",
											Template: &trainer.JobTemplatePatch{
												Spec: &trainer.JobSpecPatch{
													Template: &trainer.PodTemplatePatch{
														Spec: &trainer.PodSpecPatch{NodeSelector: map[string]string{"disktype": "ssd"}},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				}).
				Obj(),
			want: []trainer.PodTemplateOverride{
				{
					TargetJobs: []trainer.PodTemplateOverrideTargetJob{{Name: "trainer-node"}},
					Spec:       &trainer.PodTemplateSpecOverride{ServiceAccountName: ptrTo("user-sa")},
				},
				{
					TargetJobs: []trainer.PodTemplateOverrideTargetJob{{Name: "trainer-node"}},
					Spec:       &trainer.PodTemplateSpecOverride{NodeSelector: map[string]string{"disktype": "ssd"}},
				},
			},
		},
		"unmappable field JobSetTemplatePatch.Metadata is rejected": {
			trainJob: testingutil.MakeTrainJobWrapper("ns", "job").
				RuntimePatches([]trainer.RuntimePatch{
					{
						Manager: kueueManager,
						TrainingRuntimeSpec: &trainer.TrainingRuntimeSpecPatch{
							Template: &trainer.JobSetTemplatePatch{
								Metadata: &metav1.ObjectMeta{Labels: map[string]string{"a": "b"}},
								Spec:     &trainer.JobSetSpecPatch{},
							},
						},
					},
				}).
				Obj(),
			wantErrs: true,
		},
		"unmappable field JobTemplatePatch.Metadata is rejected": {
			trainJob: testingutil.MakeTrainJobWrapper("ns", "job").
				RuntimePatches([]trainer.RuntimePatch{
					{
						Manager: kueueManager,
						TrainingRuntimeSpec: &trainer.TrainingRuntimeSpecPatch{
							Template: &trainer.JobSetTemplatePatch{
								Spec: &trainer.JobSetSpecPatch{
									ReplicatedJobs: []trainer.ReplicatedJobPatch{
										{
											Name: "trainer-node",
											Template: &trainer.JobTemplatePatch{
												Metadata: &metav1.ObjectMeta{Labels: map[string]string{"a": "b"}},
											},
										},
									},
								},
							},
						},
					},
				}).
				Obj(),
			wantErrs: true,
		},
		"unmappable field PodSpecPatch.SecurityContext is rejected": {
			trainJob: testingutil.MakeTrainJobWrapper("ns", "job").
				RuntimePatches([]trainer.RuntimePatch{
					{
						Manager: kueueManager,
						TrainingRuntimeSpec: &trainer.TrainingRuntimeSpecPatch{
							Template: &trainer.JobSetTemplatePatch{
								Spec: &trainer.JobSetSpecPatch{
									ReplicatedJobs: []trainer.ReplicatedJobPatch{
										{
											Name: "trainer-node",
											Template: &trainer.JobTemplatePatch{
												Spec: &trainer.JobSpecPatch{
													Template: &trainer.PodTemplatePatch{
														Spec: &trainer.PodSpecPatch{
															SecurityContext: &corev1PodSecurityContext,
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				}).
				Obj(),
			wantErrs: true,
		},
		"unmappable field ContainerPatch.SecurityContext is rejected": {
			trainJob: testingutil.MakeTrainJobWrapper("ns", "job").
				RuntimePatches([]trainer.RuntimePatch{
					{
						Manager: kueueManager,
						TrainingRuntimeSpec: &trainer.TrainingRuntimeSpecPatch{
							Template: &trainer.JobSetTemplatePatch{
								Spec: &trainer.JobSetSpecPatch{
									ReplicatedJobs: []trainer.ReplicatedJobPatch{
										{
											Name: "trainer-node",
											Template: &trainer.JobTemplatePatch{
												Spec: &trainer.JobSpecPatch{
													Template: &trainer.PodTemplatePatch{
														Spec: &trainer.PodSpecPatch{
															Containers: []trainer.ContainerPatch{
																{
																	Name:            "node",
																	SecurityContext: &corev1ContainerSecurityContext,
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				}).
				Obj(),
			wantErrs: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, errs := EffectivePodTemplateOverrides(tc.trainJob)
			if tc.wantErrs {
				if len(errs) == 0 {
					t.Errorf("EffectivePodTemplateOverrides() returned no errors, want at least one for an unmappable field")
				}
				return
			}
			if len(errs) != 0 {
				t.Errorf("EffectivePodTemplateOverrides() returned unexpected errors: %v", errs)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Unexpected PodTemplateOverrides (-want,+got):\n%s", diff)
			}
		})
	}
}

func ptrTo[T any](v T) *T { return &v }

var (
	corev1PodSecurityContext       = corev1.PodSecurityContext{RunAsNonRoot: ptrTo(true)}
	corev1ContainerSecurityContext = corev1.SecurityContext{RunAsNonRoot: ptrTo(true)}
)
