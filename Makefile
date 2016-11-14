BUILDTAGS=

PROJECT=github.com/hustcat/oci-torrent

GIT_COMMIT := $(shell git rev-parse HEAD 2> /dev/null || true)
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2> /dev/null)

LDFLAGS := -X github.com/hustcat/oci-torrent/version.GitCommit=${GIT_COMMIT} ${LDFLAGS}

TEST_TIMEOUT ?= 5m
TEST_SUITE_TIMEOUT ?= 10m

RUNTIME ?= runc

# if this session isn't interactive, then we don't want to allocate a
# TTY, which would fail, but if it is interactive, we do want to attach
# so that the user can send e.g. ^C through.
INTERACTIVE := $(shell [ -t 0 ] && echo 1 || echo 0)
ifeq ($(INTERACTIVE), 1)
	DOCKER_FLAGS += -t
endif

DOCKER_IMAGE := oci-torrent-dev$(if $(GIT_BRANCH),:$(GIT_BRANCH))
DOCKER_RUN := docker run --privileged --rm -i $(DOCKER_FLAGS) "$(DOCKER_IMAGE)"


export GOPATH:=$(CURDIR)/vendor:$(GOPATH)

all: binary

static: binary-static

bin:
	mkdir -p bin/

clean:
	rm -rf bin && rm -rf output

binary: bin
	docker build ${DOCKER_BUILD_ARGS} -f Dockerfile.build -t ${DOCKER_IMAGE} .
	docker run --rm --security-opt label:disable -v $$(pwd):/src/github.com/hustcat/oci-torrent ${DOCKER_IMAGE} make binary-local

binary-static: bin
	docker build ${DOCKER_BUILD_ARGS} -f Dockerfile.build -t ${DOCKER_IMAGE} .
	docker run --rm --security-opt label:disable -v $$(pwd):/src/github.com/hustcat/oci-torrent ${DOCKER_IMAGE} make binary-static-local

binary-local: client daemon

binary-static-local: client-static daemon-static

client:
	cd cmd/ctr && go build -ldflags "${LDFLAGS}" -o ../../bin/oci-torrent-ctr

client-static:
	cd cmd/ctr && go build -ldflags "-w -extldflags -static ${LDFLAGS}" -tags "$(BUILDTAGS)" -o ../../bin/oci-torrent-ctr

daemon:
	cd cmd/daemon && go build -ldflags "${LDFLAGS}"  -tags "$(BUILDTAGS)"  -o ../../bin/oci-torrentd

daemon-static:
	cd cmd/daemon && go build -ldflags "-w -extldflags -static ${LDFLAGS}" -tags "$(BUILDTAGS)" -o ../../bin/oci-torrentd

install:
	cp bin/* /usr/local/bin/

protoc:
	protoc -I ./api/grpc/types ./api/grpc/types/api.proto --go_out=plugins=grpc:api/grpc/types

fmt:
	@gofmt -s -l . | grep -v vendor | grep -v .pb. | tee /dev/stderr

lint:
	@hack/validate-lint

validate: fmt lint

uninstall:
	$(foreach file,oci-torrentd ctr,rm /usr/local/bin/$(file);)
