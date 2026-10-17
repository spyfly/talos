// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package containers

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cosi-project/runtime/pkg/controller"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/cosi-project/runtime/pkg/resource/typed"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"

	"github.com/siderolabs/talos/pkg/machinery/proto"
)

// InstanceID builds the ID of a ContainerInstanceSpec from a container name and generation.
//
// Generations are numbered rather than reusing the container name so that each execution attempt has
// an identity of its own: a status then refers unambiguously to one attempt, and the instance created
// to replace another cannot be confused with it, nor collide with a destruction still in flight.
func InstanceID(container string, generation uint64) resource.ID {
	return fmt.Sprintf("%s-%d", container, generation)
}

// ContainerInstanceSpecType is type of ContainerInstanceSpec resource.
const ContainerInstanceSpecType = resource.Type("ContainerInstanceSpecs.containers.talos.dev")

// ContainerInstanceSpec resource represents a single execution attempt of a container.
//
// Its existence is the instruction to run; its destruction is the instruction to stop. Restart is
// therefore a resource event rather than a loop inside a goroutine: the previous instance
// terminates, and the next generation replaces it.
//
// The ID is <container>-<generation>; see InstanceID.
type ContainerInstanceSpec = typed.Resource[ContainerInstanceSpecSpec, ContainerInstanceSpecExtension]

// ContainerInstanceSpecSpec is the spec for ContainerInstanceSpec.
//
// It carries a resolved snapshot of everything needed to run one execution, so whatever runs it
// never has to re-read the container spec or image status. That keeps the execution independent of
// later changes to those inputs: a spec change destroys this instance rather than mutating it.
//
//gotagsrewrite:gen
type ContainerInstanceSpecSpec struct {
	// ContainerID is the name of the owning container, i.e. the ContainerSpec ID.
	ContainerID string `yaml:"containerID" protobuf:"1"`
	// Generation is this instance's sequence number for that container.
	Generation uint64 `yaml:"generation" protobuf:"2"`

	// Image is the digest-resolved reference to run.
	Image string `yaml:"image" protobuf:"3"`

	Entrypoint  []string           `yaml:"entrypoint,omitempty" protobuf:"4"`
	Args        []string           `yaml:"args,omitempty" protobuf:"5"`
	WorkingDir  string             `yaml:"workingDir,omitempty" protobuf:"6"`
	RunAs       ContainerRunAsSpec `yaml:"runAs,omitempty" protobuf:"7"`
	Environment []string           `yaml:"environment,omitempty" protobuf:"8"`

	// Mounts are fully resolved, with host source paths filled in.
	Mounts []ResolvedMountSpec `yaml:"mounts,omitempty" protobuf:"9"`

	Security  ContainerSecuritySpec  `yaml:"security,omitempty" protobuf:"10"`
	Network   ContainerNetworkSpec   `yaml:"network,omitempty" protobuf:"11"`
	Resources ContainerResourcesSpec `yaml:"resources,omitempty" protobuf:"12"`

	// NetnsSourceInstance pins the exact ContainerInstanceSpec ID ("<container>-<generation>") of
	// the container whose network namespace this instance joins; empty when it has its own.
	//
	// Each generation runs in a freshly created network namespace, so joining "the target" means
	// joining one generation of it; the pin is what makes a joined container follow the target
	// across target restarts: the next generation's ID fails the sync comparison and the joined
	// instance is replaced into the new namespace.
	NetnsSourceInstance string `yaml:"netnsSourceInstance,omitempty" protobuf:"13"`
}

// ResolvedMountSpec is a mount with its host-side source resolved.
//
//gotagsrewrite:gen
type ResolvedMountSpec struct {
	Kind string `yaml:"kind" protobuf:"1"`
	// Source is the host path to bind from; empty for tmpfs and userVolume.
	Source      string   `yaml:"source,omitempty" protobuf:"2"`
	Destination string   `yaml:"destination" protobuf:"3"`
	Size        uint64   `yaml:"size,omitempty" protobuf:"4"`
	Options     []string `yaml:"options,omitempty" protobuf:"5"`
	// VolumeID is the resolved userVolume's ID; empty for tmpfs and hostPath.
	VolumeID string `yaml:"volumeID,omitempty" protobuf:"6"`
}

// NewContainerInstanceSpec initializes a ContainerInstanceSpec resource.
func NewContainerInstanceSpec(namespace resource.Namespace, id resource.ID) *ContainerInstanceSpec {
	return typed.NewResource[ContainerInstanceSpecSpec, ContainerInstanceSpecExtension](
		resource.NewMetadata(namespace, ContainerInstanceSpecType, id, resource.VersionUndefined),
		ContainerInstanceSpecSpec{},
	)
}

// ContainerInstanceSpecExtension is auxiliary resource data for ContainerInstanceSpec.
type ContainerInstanceSpecExtension struct{}

// ResourceDefinition implements meta.ResourceDefinitionProvider interface.
func (ContainerInstanceSpecExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             ContainerInstanceSpecType,
		Aliases:          []resource.Type{"containerinstancespec", "containerinstancespecs"},
		DefaultNamespace: NamespaceName,
		PrintColumns: []meta.PrintColumn{
			{
				Name:     "Container",
				JSONPath: `{.containerID}`,
			},
			{
				Name:     "Generation",
				JSONPath: `{.generation}`,
			},
			{
				Name:     "Image",
				JSONPath: `{.image}`,
			},
		},
	}
}

func init() {
	proto.RegisterDefaultTypes()

	if err := protobuf.RegisterDynamic(ContainerInstanceSpecType, &ContainerInstanceSpec{}); err != nil {
		panic(err)
	}
}

// ContainerInstanceStatusType is type of ContainerInstanceStatus resource.
const ContainerInstanceStatusType = resource.Type("ContainerInstanceStatuses.containers.talos.dev")

// ContainerInstanceStatus resource reports the execution state of a ContainerInstanceSpec.
//
// It is produced by RuntimeController, the only component that actually runs the instance's task.
// The ID matches the ContainerInstanceSpec it reports on.
type ContainerInstanceStatus = typed.Resource[ContainerInstanceStatusSpec, ContainerInstanceStatusExtension]

// ContainerInstanceStatusSpec is the spec for ContainerInstanceStatus.
//
//gotagsrewrite:gen
type ContainerInstanceStatusSpec struct {
	// ContainerID is the name of the owning container, i.e. the ContainerSpec ID.
	ContainerID string `yaml:"containerID" protobuf:"1"`
	// Generation is the reported instance's sequence number for that container.
	Generation uint64 `yaml:"generation" protobuf:"2"`
	// Phase is the current execution phase.
	Phase ContainerInstancePhase `yaml:"phase" protobuf:"3"`
	// PID is the task's process ID while running.
	PID uint32 `yaml:"pid,omitempty" protobuf:"4"`
	// ExitCode is the task's exit code, meaningful only once Phase is ContainerInstancePhaseTerminated.
	ExitCode int32 `yaml:"exitCode,omitempty" protobuf:"5"`
	// Error describes why the task never started or exited abnormally.
	Error string `yaml:"error,omitempty" protobuf:"6"`
	// StartedAt is when the task's process started.
	StartedAt time.Time `yaml:"startedAt,omitempty" protobuf:"7"`
	// FinishedAt is when the task stopped running.
	FinishedAt time.Time `yaml:"finishedAt,omitempty" protobuf:"8"`
}

// NewContainerInstanceStatus initializes a ContainerInstanceStatus resource.
func NewContainerInstanceStatus(namespace resource.Namespace, id resource.ID) *ContainerInstanceStatus {
	return typed.NewResource[ContainerInstanceStatusSpec, ContainerInstanceStatusExtension](
		resource.NewMetadata(namespace, ContainerInstanceStatusType, id, resource.VersionUndefined),
		ContainerInstanceStatusSpec{},
	)
}

// ContainerInstanceStatusExtension is auxiliary resource data for ContainerInstanceStatus.
type ContainerInstanceStatusExtension struct{}

// ResourceDefinition implements meta.ResourceDefinitionProvider interface.
func (ContainerInstanceStatusExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             ContainerInstanceStatusType,
		Aliases:          []resource.Type{"containerinstancestatus", "containerinstancestatuses"},
		DefaultNamespace: NamespaceName,
		PrintColumns: []meta.PrintColumn{
			{
				Name:     "Container",
				JSONPath: `{.containerID}`,
			},
			{
				Name:     "Phase",
				JSONPath: `{.phase}`,
			},
			{
				Name:     "PID",
				JSONPath: `{.pid}`,
			},
			{
				Name:     "Exit Code",
				JSONPath: `{.exitCode}`,
			},
		},
	}
}

func init() {
	proto.RegisterDefaultTypes()

	if err := protobuf.RegisterDynamic(ContainerInstanceStatusType, &ContainerInstanceStatus{}); err != nil {
		panic(err)
	}
}

// InSyncWithContainerSpec reports whether the instance still matches the given container spec.
//
// The instance carries a resolved snapshot precisely so this comparison is possible: a running
// container is never mutated in place, it is replaced.
func (instanceSpec ContainerInstanceSpecSpec) InSyncWithContainerSpec(ctx context.Context, r controller.Reader, containerSpec *ContainerSpecSpec) (bool, error) {
	imageDigest, err := GetImageDigest(ctx, r, instanceSpec.ContainerID, containerSpec.Image.Ref)
	if err != nil {
		return false, err
	}

	if imageDigest != "" && imageDigest != instanceSpec.Image {
		return false, nil
	}

	if containerSpec.Network.NetnsContainer != "" {
		netnsSource, err := ResolveNetnsSourceInstance(ctx, r, containerSpec.Network.NetnsContainer)
		if err != nil {
			return false, err
		}

		if netnsSource != "" && netnsSource != instanceSpec.NetnsSourceInstance {
			// The target container's newest running generation changed, which means it moved to a
			// freshly created network namespace: the joined instance follows it by being replaced.
			//
			// While the target has no running instance at all (it is being replaced), the pin is
			// stale by definition but so is every other choice; keeping the instance running, as
			// with an unresolved image digest, avoids flapping and the replacement happens as soon
			// as the target is back.
			return false, nil
		}
	}

	expectedResolvedMounts, err := containerSpec.GetResolvedMounts(ctx, r, instanceSpec.ContainerID)
	if err != nil {
		return false, err
	}

	if !MountsResolvedMatchDeclared(expectedResolvedMounts, containerSpec.Mounts) {
		// Nothing to compare against, and a mount that has gone away is not drift to be fixed by
		// replacing the instance: it is handled by the container being stopped.
		return false, nil
	}

	inSync := containerSpec.InstanceProcessEqual(instanceSpec) &&
		ResolvedMountsEqual(expectedResolvedMounts, instanceSpec.Mounts) &&
		containerSpec.Security.Equal(instanceSpec.Security) &&
		containerSpec.Network == instanceSpec.Network &&
		containerSpec.Resources == instanceSpec.Resources

	return inSync, nil
}

// GetImageDigest returns the digest an instance of this container should run, or an empty string if
// imageRef has not been resolved to one.
//
// The status is only believed when it describes imageRef itself: an edited reference leaves the
// previous reference's status in place until ImageController re-pulls, and running those bytes under
// the new configuration would be running an image nobody asked for.
func GetImageDigest(ctx context.Context, r controller.Reader, containerID, imageRef string) (string, error) {
	imageStatus, err := safe.ReaderGetByID[*ContainerImageStatus](ctx, r, containerID)
	if err != nil {
		if state.IsNotFoundError(err) {
			return "", nil
		}

		return "", fmt.Errorf("failed to get image status %q: %w", containerID, err)
	}

	if imageStatus.TypedSpec().Phase != ContainerImagePhaseReady || imageStatus.TypedSpec().Image != imageRef {
		return "", nil
	}

	return imageStatus.TypedSpec().Digest, nil
}

// ResolveNetnsSourceInstance returns the ID of the newest running instance of targetContainer,
// which a joining container should use as its network namespace source, or an empty string if the
// target currently has no running instance.
//
// Only a running generation is a valid source: the target's network namespace exists exactly as
// long as its task does, and each generation of the target runs in a fresh one. Older generations
// are only checked when a newer one is not running, to cover the moment where a replacement has
// been created but has not started yet.
func ResolveNetnsSourceInstance(ctx context.Context, r controller.Reader, targetContainer string) (string, error) {
	if targetContainer == "" {
		return "", nil
	}

	instanceSpecs, err := safe.ReaderListAll[*ContainerInstanceSpec](ctx, r)
	if err != nil {
		return "", fmt.Errorf("failed to list container instances: %w", err)
	}

	matching := []*ContainerInstanceSpec{}

	for instanceSpec := range instanceSpecs.All() {
		if instanceSpec.TypedSpec().ContainerID == targetContainer {
			matching = append(matching, instanceSpec)
		}
	}

	// Highest generation first, so the newest one with a running task wins.
	slices.SortFunc(matching, func(a, b *ContainerInstanceSpec) int {
		return cmp.Compare(b.TypedSpec().Generation, a.TypedSpec().Generation)
	})

	for _, instanceSpec := range matching {
		status, err := safe.ReaderGetByID[*ContainerInstanceStatus](ctx, r, instanceSpec.Metadata().ID())
		if err != nil {
			if state.IsNotFoundError(err) {
				continue
			}

			return "", fmt.Errorf("failed to get instance status %q: %w", instanceSpec.Metadata().ID(), err)
		}

		if status.TypedSpec().Phase == ContainerInstancePhaseRunning && status.TypedSpec().PID != 0 {
			return instanceSpec.Metadata().ID(), nil
		}
	}

	return "", nil
}

// Int32PtrEqual compares two int32 pointers, treating nil as equal only to nil.
func Int32PtrEqual(a, b *int32) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

// ResolvedMountsEqual compares two resolved mount lists field by field, as they carry slices.
func ResolvedMountsEqual(a, b []ResolvedMountSpec) bool {
	return slices.EqualFunc(a, b, func(x, y ResolvedMountSpec) bool {
		return x.Kind == y.Kind &&
			x.Source == y.Source &&
			x.Destination == y.Destination &&
			x.Size == y.Size &&
			x.VolumeID == y.VolumeID &&
			slices.Equal(x.Options, y.Options)
	})
}
