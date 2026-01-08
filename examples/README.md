# Examples

You can run the function locally and test it using `crossplane beta render` with these example manifests.

Note that the function needs to run against an existing Grafana instance. Create a providerConfig and secret similar to `<example>/extra/stub.yaml` (please do not commit it). Then reference it in the `<example>/xr.yaml`.

Examples generally have a `run.sh` as a convenience for running the render command.

Example output:

```shell
# Run the function locally
$ go run . --insecure --debug
```

```shell
# Then, in another terminal, call it with these example manifests
$ crossplane beta render xr.yaml composition.yaml functions.yaml -r
---
apiVersion: example.crossplane.io/v1
kind: XR
metadata:
  name: example-xr
---
apiVersion: render.crossplane.io/v1beta1
kind: Result
message: I was run with input "Hello world"!
severity: SEVERITY_NORMAL
step: run-the-template
```
