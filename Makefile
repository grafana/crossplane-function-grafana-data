lint:
	docker run --rm -v $$(pwd):/app -w /app golangci/golangci-lint:v2.5.0 golangci-lint run

fmt:
	gci write \
		-s standard \
		-s default \
		-s 'prefix(github.com/crossplane)' \
		-s 'prefix(github.com/crossplane-contrib)' \
		-s blank \
		-s dot \
		$$(pwd)
