// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package containers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cosi-project/runtime/pkg/state"
	"github.com/cosi-project/runtime/pkg/state/impl/inmem"
	"github.com/cosi-project/runtime/pkg/state/impl/namespaced"

	"github.com/siderolabs/talos/pkg/machinery/resources/containers"
)

func TestResolveNetnsSourceInstance(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	st := state.WrapCore(namespaced.NewState(inmem.Build))

	// A mixed set of instances: two running generations of one container, a newer generation that
	// has not started yet, and a terminated one, so the resolver has to pick by generation and by
	// actually running rather than by either alone.
	for _, instance := range []struct {
		container  string
		generation uint64
		phase      containers.ContainerInstancePhase
		pid        uint32
	}{
		{container: "running", generation: 0, phase: containers.ContainerInstancePhaseRunning, pid: 100},
		{container: "running", generation: 1, phase: containers.ContainerInstancePhaseRunning, pid: 200},
		{container: "starting", generation: 0, phase: containers.ContainerInstancePhaseCreated},
		{container: "stopped", generation: 0, phase: containers.ContainerInstancePhaseTerminated},
		{container: "replacing", generation: 0, phase: containers.ContainerInstancePhaseRunning, pid: 300},
		{container: "replacing", generation: 1, phase: containers.ContainerInstancePhaseCreated},
	} {
		id := containers.InstanceID(instance.container, instance.generation)

		instanceSpec := containers.NewContainerInstanceSpec(containers.NamespaceName, id)
		instanceSpec.TypedSpec().ContainerID = instance.container
		instanceSpec.TypedSpec().Generation = instance.generation
		require.NoError(t, st.Create(ctx, instanceSpec))

		status := containers.NewContainerInstanceStatus(containers.NamespaceName, id)
		status.TypedSpec().ContainerID = instance.container
		status.TypedSpec().Generation = instance.generation
		status.TypedSpec().Phase = instance.phase
		status.TypedSpec().PID = instance.pid
		require.NoError(t, st.Create(ctx, status))
	}

	for _, test := range []struct {
		name   string
		target string
		want   string
	}{
		{name: "newest running generation wins", target: "running", want: "running-1"},
		{name: "instance that never started is not a source", target: "starting", want: ""},
		{name: "terminated instance is not a source", target: "stopped", want: ""},
		{name: "falls back to the newest running generation while a replacement starts", target: "replacing", want: "replacing-0"},
		{name: "unknown container", target: "ghost", want: ""},
		{name: "empty target", target: "", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := containers.ResolveNetnsSourceInstance(ctx, st, test.target)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}
