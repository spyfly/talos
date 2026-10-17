// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package container_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/siderolabs/talos/pkg/machinery/config/config"
	containercfg "github.com/siderolabs/talos/pkg/machinery/config/types/container"
)

func TestContainerNetworkValidate(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		mode        config.ContainerNetworkMode
		expectedErr string
	}{
		{
			name: "defaults to none",
		},
		{
			name: "none",
			mode: config.ContainerNetworkModeNone,
		},
		{
			name: "host",
			mode: config.ContainerNetworkModeHost,
		},
		{
			name: "joins another container",
			mode: "container:target",
		},
		{
			name: "joins a container with digits and hyphens",
			mode: "container:my-app-2",
		},
		{
			name:        "unsupported mode",
			mode:        "bridge",
			expectedErr: `unsupported network mode "bridge", expected none, host or container:<name>`,
		},
		{
			name:        "container prefix without a name",
			mode:        "container:",
			expectedErr: `unsupported network mode "container:", expected none, host or container:<name>`,
		},
		{
			name:        "container name with uppercase letters",
			mode:        "container:Target",
			expectedErr: `container name "Target" can only contain lowercase ASCII letters, digits and hyphens`,
		},
		{
			name:        "container name with a path separator",
			mode:        "container:ns/target",
			expectedErr: `container name "ns/target" can only contain lowercase ASCII letters, digits and hyphens`,
		},
		{
			name:        "container name too long",
			mode:        config.ContainerNetworkMode("container:" + strings.Repeat("a", 64)),
			expectedErr: `container name "` + strings.Repeat("a", 64) + `" must be 63 characters or fewer`,
		},
		{
			name: "container name of maximum length",
			mode: config.ContainerNetworkMode("container:" + strings.Repeat("a", 63)),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			network := &containercfg.ContainerNetwork{NetworkMode: test.mode}

			err := network.Validate()

			if test.expectedErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), test.expectedErr)
		})
	}
}

func TestContainerNetworkNetnsContainer(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		mode config.ContainerNetworkMode
		want string
	}{
		{
			name: "none",
			mode: config.ContainerNetworkModeNone,
		},
		{
			name: "host",
			mode: config.ContainerNetworkModeHost,
		},
		{
			name: "empty",
		},
		{
			name: "container join",
			mode: "container:target",
			want: "target",
		},
		{
			name: "container prefix without a name",
			mode: "container:",
		},
		{
			name: "unrelated mode does not match the prefix",
			mode: "host",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			network := &containercfg.ContainerNetwork{NetworkMode: test.mode}

			assert.Equal(t, test.want, network.NetnsContainer())
		})
	}
}
