OS ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)

IMAGE_NAME := cert-manager-webhook-namecheap
IMAGE_TAG := $(shell git describe --dirty)
REPO_NAME := extrality
PLATFORMS := linux/amd64,linux/arm64

OUT := $(shell pwd)/_out

KUBEBUILDER_VERSION=2.3.2

$(shell mkdir -p "$(OUT)")

test: _test/kubebuilder
	go test -v .

_test/kubebuilder:
	curl -fsSL https://github.com/kubernetes-sigs/kubebuilder/releases/download/v$(KUBEBUILDER_VERSION)/kubebuilder_$(KUBEBUILDER_VERSION)_$(OS)_$(ARCH).tar.gz -o kubebuilder-tools.tar.gz
	mkdir -p _test/kubebuilder
	tar -xvf kubebuilder-tools.tar.gz
	mv kubebuilder_$(KUBEBUILDER_VERSION)_$(OS)_$(ARCH)/bin _test/kubebuilder/
	rm kubebuilder-tools.tar.gz
	rm -R kubebuilder_$(KUBEBUILDER_VERSION)_$(OS)_$(ARCH)

clean: clean-kubebuilder

clean-kubebuilder:
	rm -Rf _test/kubebuilder

tag:
	docker buildx build --platform $(PLATFORMS) -t "$(REPO_NAME)/$(IMAGE_NAME):latest" .
	docker buildx build --platform $(PLATFORMS) -t "$(REPO_NAME)/$(IMAGE_NAME):$(IMAGE_TAG)" .

push:
	docker buildx build --push --platform $(PLATFORMS) -t "$(REPO_NAME)/$(IMAGE_NAME):latest" .
	docker buildx build --push --platform $(PLATFORMS) -t "$(REPO_NAME)/$(IMAGE_NAME):$(IMAGE_TAG)" .


.PHONY: rendered-manifest.yaml
rendered-manifest.yaml:
	helm template \
	    --name ${IMAGE_NAME} \
        --set image.repository=$(IMAGE_NAME) \
        --set image.tag=$(IMAGE_TAG) \
        deploy/${IMAGE_NAME} > "$(OUT)/rendered-manifest.yaml"
