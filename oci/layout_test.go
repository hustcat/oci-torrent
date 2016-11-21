package oci

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/net/context"
)

func TestOpen(t *testing.T) {
	ctx := context.Background()

	root, err := ioutil.TempDir("", "oci-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	image := filepath.Join(root, "busybox")

	layout, err := Open(image)
	if err != nil {
		t.Fatalf("unexpected error opening image: %s", err)
	}
	defer layout.Close()

	// We should have no references or blobs.
	if refs, err := layout.ListReferences(ctx); err != nil {
		t.Errorf("unexpected error getting list of references: %s", err)
	} else if len(refs) > 0 {
		t.Errorf("got references in a newly created image: %v", refs)
	}
	if blobs, err := layout.ListBlobs(ctx); err != nil {
		t.Errorf("unexpected error getting list of blobs: %s", err)
	} else if len(blobs) > 0 {
		t.Errorf("got blobs in a newly created image: %v", blobs)
	}
}

func TestBlob(t *testing.T) {
	ctx := context.Background()

	root, err := ioutil.TempDir("", "oci-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	image := filepath.Join(root, "busybox")

	layout, err := Open(image)
	if err != nil {
		t.Fatalf("unexpected error opening image: %s", err)
	}
	defer layout.Close()

	for _, test := range []struct {
		bytes []byte
	}{
		{[]byte("")},
		{[]byte("some blob")},
		{[]byte("another blob")},
	} {
		hash := sha256.New()
		if _, err := io.Copy(hash, bytes.NewReader(test.bytes)); err != nil {
			t.Fatalf("could not hash bytes: %s", err)
		}
		expectedDigest := fmt.Sprintf("%s:%x", BlobAlgorithm, hash.Sum(nil))

		digest, size, err := layout.PutBlob(ctx, bytes.NewReader(test.bytes))
		if err != nil {
			t.Errorf("PutBlob: unexpected error: %s", err)
		}

		if digest != expectedDigest {
			t.Errorf("PutBlob: digest doesn't match: expected=%s got=%s", expectedDigest, digest)
		}
		if size != int64(len(test.bytes)) {
			t.Errorf("PutBlob: length doesn't match: expected=%d got=%d", len(test.bytes), size)
		}

		blobReader, err := layout.GetBlob(ctx, digest)
		if err != nil {
			t.Errorf("GetBlob: unexpected error: %s", err)
		}
		defer blobReader.Close()

		gotBytes, err := ioutil.ReadAll(blobReader)
		if err != nil {
			t.Errorf("GetBlob: failed to ReadAll: %s", err)
		}
		if !bytes.Equal(test.bytes, gotBytes) {
			t.Errorf("GetBlob: bytes did not match: expected=%s got=%s", string(test.bytes), string(gotBytes))
		}

		if err := layout.DeleteBlob(ctx, digest); err != nil {
			t.Errorf("DeleteBlob: unexpected error: %s", err)
		}

		if br, err := layout.GetBlob(ctx, digest); !os.IsNotExist(err) {
			if err == nil {
				br.Close()
				t.Errorf("GetBlob: still got blob contents after DeleteBlob!")
			} else {
				t.Errorf("GetBlob: unexpected error: %s", err)
			}
		}

		// DeleteBlob is idempotent. It shouldn't cause an error.
		if err := layout.DeleteBlob(ctx, digest); err != nil {
			t.Errorf("DeleteBlob: unexpected error on double-delete: %s", err)
		}
	}

	// Should be no blobs left.
	if blobs, err := layout.ListBlobs(ctx); err != nil {
		t.Errorf("unexpected error getting list of blobs: %s", err)
	} else if len(blobs) > 0 {
		t.Errorf("got blobs in a clean image: %v", blobs)
	}
}

func TestReference(t *testing.T) {
	ctx := context.Background()

	root, err := ioutil.TempDir("", "oci-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	image := filepath.Join(root, "busybox")

	layout, err := Open(image)
	if err != nil {
		t.Fatalf("unexpected error opening image: %s", err)
	}
	defer layout.Close()

	for _, test := range []struct {
		name       string
		descriptor v1.Descriptor
	}{
		{"ref1", v1.Descriptor{}},
		{"ref2", v1.Descriptor{MediaType: v1.MediaTypeImageConfig, Digest: "sha256:032581de4629652b8653e4dbb2762d0733028003f1fc8f9edd61ae8181393a15", Size: 100}},
		{"ref3", v1.Descriptor{MediaType: v1.MediaTypeImageLayerNonDistributable, Digest: "sha256:3c968ad60d3a2a72a12b864fa1346e882c32690cbf3bf3bc50ee0d0e4e39f342", Size: 8888}},
	} {
		if err := layout.PutReference(ctx, test.name, &test.descriptor); err != nil {
			t.Errorf("PutReference: unexpected error: %s", err)
		}

		gotDescriptor, err := layout.GetReference(ctx, test.name)
		if err != nil {
			t.Errorf("GetReference: unexpected error: %s", err)
		}

		if !reflect.DeepEqual(test.descriptor, *gotDescriptor) {
			t.Errorf("GetReference: got different descriptor to original: expected=%v got=%v", test.descriptor, gotDescriptor)
		}

		if err := layout.DeleteReference(ctx, test.name); err != nil {
			t.Errorf("DeleteReference: unexpected error: %s", err)
		}

		if _, err := layout.GetReference(ctx, test.name); !os.IsNotExist(err) {
			if err == nil {
				t.Errorf("GetReference: still got reference descriptor after DeleteReference!")
			} else {
				t.Errorf("GetReference: unexpected error: %s", err)
			}
		}

		// DeleteBlob is idempotent. It shouldn't cause an error.
		if err := layout.DeleteReference(ctx, test.name); err != nil {
			t.Errorf("DeleteReference: unexpected error on double-delete: %s", err)
		}
	}
}
