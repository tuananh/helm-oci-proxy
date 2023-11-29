helm-oci-proxy
--------------

This take a legacy Helm repo and serve it as OCI registry.

## Features

- Turn legacy Helm repo into OCI Helm repo
- Cache with GCS.

## Usage

I'm using Argo Helm as an example here.

```sh
BUCKET=my-gcs-bucket REPO_URL=https://argoproj.github.io/argo-helm go run ./cmd
```

Try pulling your helm chart

```sh
helm pull oci://localhost:5000/argo-cd --version 5.51.3
```

It should works.

```sh
$ helm pull oci://localhost:5000/argo-cd --version 5.51.3
Pulled: localhost:5000/argo-cd:5.51.3
Digest: sha256:4628ea153f308dccceb435e28b9ffcfda0af7eba3c53fd4d6d328323ee71c5fc
$ tar --list -f argo-cd-5.51.3.tgz | head
argo-cd/Chart.yaml
argo-cd/Chart.lock
argo-cd/values.yaml
argo-cd/templates/NOTES.txt
argo-cd/templates/_common.tpl
argo-cd/templates/_helpers.tpl
argo-cd/templates/_versions.tpl
argo-cd/templates/aggregate-roles.yaml
argo-cd/templates/argocd-application-controller/clusterrole.yaml
argo-cd/templates/argocd-application-controller/clusterrolebinding.yaml
```

## License

Copyright 2023 Tuan Anh Tran <me@tuananh.org>

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the “Software”), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
