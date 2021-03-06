package docker

import (
	"fmt"
	"strings"

	"github.com/containers/image/docker/policyconfiguration"
	"github.com/containers/image/docker/reference"
	"github.com/containers/image/types"
)

// Transport is an ImageTransport for Docker registry-hosted images.
var Transport = dockerTransport{}

type dockerTransport struct{}

func (t dockerTransport) Name() string {
	return "docker"
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an ImageReference.
func (t dockerTransport) ParseReference(reference string) (types.ImageReference, error) {
	return ParseReference(reference)
}

// ValidatePolicyConfigurationScope checks that scope is a valid name for a signature.PolicyTransportScopes keys
// (i.e. a valid PolicyConfigurationIdentity() or PolicyConfigurationNamespaces() return value).
// It is acceptable to allow an invalid value which will never be matched, it can "only" cause user confusion.
// scope passed to this function will not be "", that value is always allowed.
func (t dockerTransport) ValidatePolicyConfigurationScope(scope string) error {
	// FIXME? We could be verifying the various character set and length restrictions
	// from docker/distribution/reference.regexp.go, but other than that there
	// are few semantically invalid strings.
	return nil
}

// dockerReference is an ImageReference for Docker images.
type dockerReference struct {
	ref reference.Named // By construction we know that !reference.IsNameOnly(ref)
}

// ParseReference converts a string, which should not start with the ImageTransport.Name prefix, into an Docker ImageReference.
func ParseReference(refString string) (types.ImageReference, error) {
	if !strings.HasPrefix(refString, "//") {
		return nil, fmt.Errorf("docker: image reference %s does not start with //", refString)
	}
	ref, err := reference.ParseNamed(strings.TrimPrefix(refString, "//"))
	if err != nil {
		return nil, err
	}
	ref = reference.WithDefaultTag(ref)
	return NewReference(ref)
}

// NewReference returns a Docker reference for a named reference. The reference must satisfy !reference.IsNameOnly().
func NewReference(ref reference.Named) (types.ImageReference, error) {
	if reference.IsNameOnly(ref) {
		return nil, fmt.Errorf("Docker reference %s has neither a tag nor a digest", ref.String())
	}
	// A github.com/distribution/reference value can have a tag and a digest at the same time!
	// docker/reference does not handle that, so fail.
	// (Even if it were supported, the semantics of policy namespaces are unclear - should we drop
	// the tag or the digest first?)
	_, isTagged := ref.(reference.NamedTagged)
	_, isDigested := ref.(reference.Canonical)
	if isTagged && isDigested {
		return nil, fmt.Errorf("Docker references with both a tag and digest are currently not supported")
	}
	return dockerReference{
		ref: ref,
	}, nil
}

func (ref dockerReference) Transport() types.ImageTransport {
	return Transport
}

// StringWithinTransport returns a string representation of the reference, which MUST be such that
// reference.Transport().ParseReference(reference.StringWithinTransport()) returns an equivalent reference.
// NOTE: The returned string is not promised to be equal to the original input to ParseReference;
// e.g. default attribute values omitted by the user may be filled in in the return value, or vice versa.
// WARNING: Do not use the return value in the UI to describe an image, it does not contain the Transport().Name() prefix.
func (ref dockerReference) StringWithinTransport() string {
	return "//" + ref.ref.String()
}

// DockerReference returns a Docker reference associated with this reference
// (fully explicit, i.e. !reference.IsNameOnly, but reflecting user intent,
// not e.g. after redirect or alias processing), or nil if unknown/not applicable.
func (ref dockerReference) DockerReference() reference.Named {
	return ref.ref
}

// PolicyConfigurationIdentity returns a string representation of the reference, suitable for policy lookup.
// This MUST reflect user intent, not e.g. after processing of third-party redirects or aliases;
// The value SHOULD be fully explicit about its semantics, with no hidden defaults, AND canonical
// (i.e. various references with exactly the same semantics should return the same configuration identity)
// It is fine for the return value to be equal to StringWithinTransport(), and it is desirable but
// not required/guaranteed that it will be a valid input to Transport().ParseReference().
// Returns "" if configuration identities for these references are not supported.
func (ref dockerReference) PolicyConfigurationIdentity() string {
	res, err := policyconfiguration.DockerReferenceIdentity(ref.ref)
	if res == "" || err != nil { // Coverage: Should never happen, NewReference above should refuse values which could cause a failure.
		panic(fmt.Sprintf("Internal inconsistency: policyconfiguration.DockerReferenceIdentity returned %#v, %v", res, err))
	}
	return res
}

// PolicyConfigurationNamespaces returns a list of other policy configuration namespaces to search
// for if explicit configuration for PolicyConfigurationIdentity() is not set.  The list will be processed
// in order, terminating on first match, and an implicit "" is always checked at the end.
// It is STRONGLY recommended for the first element, if any, to be a prefix of PolicyConfigurationIdentity(),
// and each following element to be a prefix of the element preceding it.
func (ref dockerReference) PolicyConfigurationNamespaces() []string {
	return policyconfiguration.DockerReferenceNamespaces(ref.ref)
}

// NewImage returns a types.Image for this reference, possibly specialized for this ImageTransport.
// The caller must call .Close() on the returned Image.
// NOTE: If any kind of signature verification should happen, build an UnparsedImage from the value returned by NewImageSource,
// verify that UnparsedImage, and convert it into a real Image via image.FromUnparsedImage.
func (ref dockerReference) NewImage(ctx *types.SystemContext) (types.Image, error) {
	return newImage(ctx, ref)
}

// NewImageSource returns a types.ImageSource for this reference,
// asking the backend to use a manifest from requestedManifestMIMETypes if possible.
// nil requestedManifestMIMETypes means manifest.DefaultRequestedManifestMIMETypes.
// The caller must call .Close() on the returned ImageSource.
func (ref dockerReference) NewImageSource(ctx *types.SystemContext, requestedManifestMIMETypes []string) (types.ImageSource, error) {
	return newImageSource(ctx, ref, requestedManifestMIMETypes)
}

// NewImageDestination returns a types.ImageDestination for this reference.
// The caller must call .Close() on the returned ImageDestination.
func (ref dockerReference) NewImageDestination(ctx *types.SystemContext) (types.ImageDestination, error) {
	return newImageDestination(ctx, ref)
}

// DeleteImage deletes the named image from the registry, if supported.
func (ref dockerReference) DeleteImage(ctx *types.SystemContext) error {
	return deleteImage(ctx, ref)
}

// tagOrDigest returns a tag or digest from the reference.
func (ref dockerReference) tagOrDigest() (string, error) {
	if ref, ok := ref.ref.(reference.Canonical); ok {
		return ref.Digest().String(), nil
	}
	if ref, ok := ref.ref.(reference.NamedTagged); ok {
		return ref.Tag(), nil
	}
	// This should not happen, NewReference above refuses reference.IsNameOnly values.
	return "", fmt.Errorf("Internal inconsistency: Reference %s unexpectedly has neither a digest nor a tag", ref.ref.String())
}
