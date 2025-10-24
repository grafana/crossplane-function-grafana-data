lint:
	docker run --rm -v $$(pwd):/app -w /app golangci/golangci-lint:v2.5.0 golangci-lint run
