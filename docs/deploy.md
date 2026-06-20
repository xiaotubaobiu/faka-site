# Deploy

faka-site ships via GitHub Actions: a tag push builds a Docker image, pushes
it to Docker Hub, and rolls the HK server onto the new image.

## Pipeline (`.github/workflows/release.yml`)

| Job           | Trigger                                | What it does                                        |
| ------------- | -------------------------------------- | --------------------------------------------------- |
| `test`        | PR to `main`, push to `main`, tag `v*` | `go vet ./...` + `go test ./...` (excludes sandbox) |
| `build-deploy`| tag `v*` only (after `test` passes)    | Build image → push to Docker Hub → SSH deploy to HK |

The `sandboxlive` build-tag tests are **not** part of CI — they need real
Alipay keys and only run on the configured server:

```
# on the HK server, from /opt/faka-site:
docker run --rm -v "$PWD":/src -v "$PWD/keys":/src/keys:ro \
  -v "$PWD/data":/src/data:ro -w /src golang:1.25-alpine \
  go test -tags sandboxlive ./internal/payment/ -run TestSandboxLive_PrecreateAndNotify -v
```

## Releasing

```bash
git checkout main && git pull
git tag v1.2.0        # semver
git push origin v1.2.0
# watch: gh run watch
```

The workflow pushes `xiaotubao/faka-site:<tag>` and `:latest`, then SSHes into
the HK server, pulls `<tag>`, and recreates the `faka-site` container with the
**same** port mapping (`127.0.0.1:8090:8080`), env (sourced from the server's
`/opt/faka-site/.env`), and bind mounts (`data/`, `keys/`). Caddy fronts it on
`https://pay.000328.xyz`.

## Required repo secrets

| Secret             | Value                                            |
| ------------------ | ------------------------------------------------ |
| `DOCKERHUB_USERNAME` | Docker Hub account (`xiaotubao`)                |
| `DOCKERHUB_TOKEN`    | Docker Hub access token                         |
| `DEPLOY_SSH_HOST`    | `103.85.224.229`                                |
| `DEPLOY_SSH_USER`    | `root`                                          |
| `DEPLOY_SSH_KEY`     | contents of the HK server's SSH private key     |

## Rollback

Pin to a previous tag and redeploy:

```bash
git tag -f v1.2.0 v1.1.0   # point the tag at the last-known-good commit
git push -f origin v1.2.0  # re-triggers the workflow → rolls back
```

Or manually on the server:

```bash
docker stop faka-site && docker rm faka-site
# re-run the docker run command from the workflow with the old tag
```

Because `data/` and `keys/` are bind-mounted and `PAY_SECRET` is unchanged,
a rollback does not lose state or make encrypted config undecryptable.
