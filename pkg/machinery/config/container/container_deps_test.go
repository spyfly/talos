// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package container_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	containercfg "github.com/siderolabs/talos/pkg/machinery/config/types/container"
)

// newContainerDoc builds a ContainerConfig document with the given dependsOn.containers list.
func newContainerDoc(name string, dependsOn ...string) *containercfg.ContainerConfigV1Alpha1 {
	doc := containercfg.NewContainerConfigV1Alpha1()
	doc.MetaName = name
	doc.ContainerImage = "docker.io/library/nginx:latest"

	if len(dependsOn) > 0 {
		doc.DependsOnConfig = &containercfg.ContainerDependsOn{
			ContainersConfig: dependsOn,
		}
	}

	return doc
}

// newNetnsContainerDoc builds a ContainerConfig document joining another container's network
// namespace, optionally with dependsOn.containers entries.
func newNetnsContainerDoc(name, netnsTarget string, dependsOn ...string) *containercfg.ContainerConfigV1Alpha1 {
	doc := newContainerDoc(name, dependsOn...)
	doc.NetworkConfig = &containercfg.ContainerNetwork{
		NetworkMode: config.ContainerNetworkMode(string(config.ContainerNetworkModeContainerPrefix) + netnsTarget),
	}

	return doc
}

// validateDocs runs container-level validation over the given documents.
func validateDocs(t *testing.T, docs ...*containercfg.ContainerConfigV1Alpha1) error {
	t.Helper()

	documents := make([]config.Document, 0, len(docs))

	for _, doc := range docs {
		documents = append(documents, doc)
	}

	ctr, err := container.New(documents...)
	require.NoError(t, err)

	_, err = ctr.ValidateAsClient(validationMode{})

	return err
}

func TestContainerDependencyCycles(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		docs        []*containercfg.ContainerConfigV1Alpha1
		expectedErr string
	}{
		{
			name: "no dependencies",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newContainerDoc("a"),
				newContainerDoc("b"),
			},
		},
		{
			name: "linear chain",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newContainerDoc("a", "b"),
				newContainerDoc("b", "c"),
				newContainerDoc("c"),
			},
		},
		{
			name: "diamond is a DAG, not a cycle",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newContainerDoc("a", "b", "c"),
				newContainerDoc("b", "d"),
				newContainerDoc("c", "d"),
				newContainerDoc("d"),
			},
		},
		{
			name: "two-node cycle",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newContainerDoc("a", "b"),
				newContainerDoc("b", "a"),
			},
			expectedErr: "container dependency cycle detected",
		},
		{
			name: "three-node cycle",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newContainerDoc("a", "b"),
				newContainerDoc("b", "c"),
				newContainerDoc("c", "a"),
			},
			expectedErr: "container dependency cycle detected",
		},
		{
			name: "dangling reference",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newContainerDoc("a", "nonexistent"),
			},
			expectedErr: `depends on container "nonexistent", which is not configured`,
		},
		{
			name: "netns join of a configured container",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newNetnsContainerDoc("a", "b"),
				newContainerDoc("b"),
			},
		},
		{
			name: "netns join with a matching dependsOn entry is not a duplicate edge",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newNetnsContainerDoc("a", "b", "b"),
				newContainerDoc("b"),
			},
		},
		{
			name: "netns join of an unconfigured container",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newNetnsContainerDoc("a", "ghost"),
			},
			expectedErr: `joins the network namespace of container "ghost", which is not configured`,
		},
		{
			name: "netns join cycle",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newNetnsContainerDoc("a", "b"),
				newNetnsContainerDoc("b", "a"),
			},
			expectedErr: "container dependency cycle detected",
		},
		{
			name: "container joining its own netns",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newNetnsContainerDoc("a", "a"),
			},
			expectedErr: "container dependency cycle detected",
		},
		{
			name: "mixed cycle through dependsOn and netns join",
			docs: []*containercfg.ContainerConfigV1Alpha1{
				newNetnsContainerDoc("a", "b"),
				newContainerDoc("b", "a"),
			},
			expectedErr: "container dependency cycle detected",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateDocs(t, test.docs...)

			if test.expectedErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.expectedErr)
		})
	}
}
