package oci

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/net/context"
)

const (
	// BlobAlgorithm is the name of the only supported digest algorithm for blobs.
	// FIXME: We can make this a list.
	BlobAlgorithm = "sha256"

	// refDirectory is the directory inside an OCI image that contains references.
	refDirectory = "refs"

	// blobDirectory is the directory inside an OCI image that contains blobs.
	blobDirectory = "blobs"

	// layoutFile is the file in side an OCI image the indicates what version
	// of the OCI spec the image is.
	layoutFile = "oci-layout"
)

// Exposed errors.
var (
	// ErrInvalid is returned when an image was detected as being invalid.
	ErrInvalid = errors.New("invalid image detected")

	// ErrNotImplemented is returned when a requested operation has not been
	// implementing the backing image store.
	ErrNotImplemented = errors.New("operation not implemented")

	// ErrClobber is returned when a requested operation would require clobbering a
	// reference or blob which already exists.
	ErrClobber = errors.New("operation would clobber existing object")
)

// Layout is an interface that provides methods for accessing and modifying an
// OCI image, namely allowing access to reference descriptors and blobs.
type Layout interface {
	// PutBlob adds a new blob to the image. This is idempotent; a nil error
	// means that "the content is stored at DIGEST" without implying "because
	// of this PutBlob() call".
	PutBlob(ctx context.Context, reader io.Reader) (digest string, size int64, err error)

	// PutReference adds a new reference descriptor blob to the image. This is
	// idempotent; a nil error means that "the descriptor is stored at NAME"
	// without implying "because of this PutReference() call". ErrClobber is
	// returned if there is already a descriptor stored at NAME, but does not
	// match the descriptor requested to be stored.
	PutReference(ctx context.Context, name string, descriptor *v1.Descriptor) (err error)

	// GetBlob returns a reader for retrieving a blob from the image, which the
	// caller must Close(). Returns os.ErrNotExist if the digest is not found.
	GetBlob(ctx context.Context, digest string) (reader io.ReadCloser, err error)

	// Exist returns true if digest is exist, else return false, which he
	// caller must Close().
	Exist(ctx context.Context, digest string) (exist bool, err error)

	// GetReference returns a reference from the image. Returns os.ErrNotExist
	// if the name was not found.
	GetReference(ctx context.Context, name string) (descriptor *v1.Descriptor, err error)

	// DeleteBlob removes a blob from the image. This is idempotent; a nil
	// error means "the content is not in the store" without implying "because
	// of this DeleteBlob() call".
	DeleteBlob(ctx context.Context, digest string) (err error)

	// DeleteReference removes a reference from the image. This is idempotent;
	// a nil error means "the content is not in the store" without implying
	// "because of this DeleteReference() call".
	DeleteReference(ctx context.Context, name string) (err error)

	// ListBlobs returns the set of blob digests stored in the image.
	ListBlobs(ctx context.Context) (digests []string, err error)

	// ListReferences returns the set of reference names stored in the image.
	ListReferences(ctx context.Context) (names []string, err error)

	// Close releases all references held by the engine. Subsequent operations
	// may fail.
	Close() (err error)

	// GetBlobPath returns a path of a blob from the image
	GetBlobPath(ctx context.Context, digest string) (path string, err error)
}

// Open will create an Layout reference to the OCI image at the provided
// path. If the image format is not supported, ErrNotImplemented will be
// returned. If the path does not exist, os.ErrNotExist will be returned.
func Open(path string) (Layout, error) {
	return newDirLayout(path)
}

// blobPath returns the path to a blob given its digest, relative to the root
// of the OCI image. The digest must be of the form algorithm:hex.
func blobPath(digest string) (string, error) {
	fields := strings.SplitN(digest, ":", 2)
	if len(fields) != 2 {
		return "", fmt.Errorf("invalid digest: %q", digest)
	}

	algo := fields[0]
	hash := fields[1]

	if algo != BlobAlgorithm {
		return "", fmt.Errorf("unsupported algorithm: %q", algo)
	}

	return filepath.Join(blobDirectory, algo, hash), nil
}

// refPath returns the path to a reference given its name, relative to the r
// oot of the OCI image.
func refPath(name string) (string, error) {
	return filepath.Join(refDirectory, name), nil
}
