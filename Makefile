BINARY := micro-device-plugin
IMAGE := your-registry/micro-device-plugin
VERSION := v1.0.0

.PHONY: build
build:
	go build -o bin/$(BINARY) ./cmd

.PHONY: docker-build
docker-build:
	docker build -t $(IMAGE):$(VERSION) .

.PHONY: push
push:
	docker push $(IMAGE):$(VERSION)

.PHONY: clean
clean:
	rm -rf bin