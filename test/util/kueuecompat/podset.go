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

package kueuecompat

import (
	corev1 "k8s.io/api/core/v1"
)

// PodSetInfo is a fallback local mirror of sigs.k8s.io/kueue's
// pkg/podset.PodSetInfo (as of release-0.18, pkg/podset/podset.go:45-54).
//
// A real test-only dependency on sigs.k8s.io/kueue was attempted first
// (`go get sigs.k8s.io/kueue@v0.18.0`, then `@latest`). Both forced a cascade
// upgrade of this module's core dependencies (k8s.io/apimachinery, client-go,
// apiextensions-apiserver, controller-runtime 0.22.3->0.24.x, sigs.k8s.io/jobset
// 0.10.1->0.12.0, etc.) that would have required a much larger, out-of-scope
// migration of this repository. That approach was reverted
// (`git checkout -- go.mod go.sum`) in favor of this local mirror.
//
// PodSetReference is mirrored as a plain string (kueue.PodSetReference is a
// `type PodSetReference string` alias) to avoid pulling in the kueue API
// package for a single type alias.
//
// Do not refactor, tidy, or improve this code beyond keeping it in sync with
// the upstream struct shape it mirrors — its value is in staying identical to
// what Kueue's PodSetInfo carries.
type PodSetInfo struct {
	Name            string
	Count           int32
	Annotations     map[string]string
	Labels          map[string]string
	NodeSelector    map[string]string
	Affinity        *corev1.Affinity
	Tolerations     []corev1.Toleration
	SchedulingGates []corev1.PodSchedulingGate
}
