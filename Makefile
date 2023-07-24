GO ?= $(shell which go)
OS ?= $(shell $(GO) env GOOS)
ARCH ?= $(shell $(GO) env GOARCH)

IMAGE_NAME := cert-manager-webhook-namecheap
IMAGE_TAG := $(shell git describe --dirty)
ifeq ($(IMAGE_TAG),)
	IMAGE_TAG := latest
endif

PLATFORMS := linux/amd64,linux/arm64
REPO_NAME := ghcr.io/extrality
OUT := $(shell pwd)/_out

KUBE_VERSION=1.25.0

$(shell mkdir -p "$(OUT)")
export TEST_ASSET_ETCD=_test/kubebuilder/etcd
export TEST_ASSET_KUBE_APISERVER=_test/kubebuilder/kube-apiserver
export TEST_ASSET_KUBECTL=_test/kubebuilder/kubectl

test: _test/kubebuilder
	$(GO) test -v .

_test/kubebuilder:
	curl -fsSL https://go.kubebuilder.io/test-tools/$(KUBE_VERSION)/$(OS)/$(ARCH) -o kubebuilder-tools.tar.gz
	mkdir -p _test/kubebuilder
	tar -xvf kubebuilder-tools.tar.gz
	mv kubebuilder/bin/* _test/kubebuilder/
	rm kubebuilder-tools.tar.gz
	rm -R kubebuilder

clean: clean-kubebuilder

clean-kubebuilder:
	rm -Rf _test/kubebuilder

build:
	docker buildx build \
		--platform $(PLATFORMS) \
		-t "$(REPO_NAME)/$(IMAGE_NAME):latest" \
		.

push:
	docker buildx build \
		--push --platform $(PLATFORMS) \
		-t "$(REPO_NAME)/$(IMAGE_NAME):latest" \
		-t "$(REPO_NAME)/$(IMAGE_NAME):$(IMAGE_TAG)" \
		.


.PHONY: rendered-manifest.yaml
rendered-manifest.yaml:
	helm template \
	    --name example-webhook \
            --set image.repository=$(IMAGE_NAME) \
            --set image.tag=$(IMAGE_TAG) \
            deploy/example-webhook > "$(OUT)/rendered-manifest.yaml"
