## e2e tests

Project uses Ginkgo testing framework that by default doesn't guarantee the order in which the tests are run.

Basic test that should work w/o any credentials for GitHub:

```bash
E2E_PRINT_LOGS=false ONLY=podinfo make e2e-test
```

Test suite that works with OTel Operator and uses a metric receiver from GitHub:

```bash
E2E_PRINT_LOGS=false PR_BRANCH=operator GH_PAT=*** make e2e-test
```

### Environment Variables

| env var              | description                                                                                                                                  |
|----------------------|----------------------------------------------------------------------------------------------------------------------------------------------|
| `E2E_PRINT_LOGS`     | will print the logs of KEDA operator, scaler and others after unsuccessful run                                                               | 
| `PR_BRANCH`          | Branch from which the PR is being made, used by [`github-so.yaml`](./testdata/github-so.yaml) (the label in the metricQuery).                |
| `GH_PAT`             | GitHub token that has read permission on repo specified in the [`github-so.yaml`](./testdata/github-so.yaml) (the label in the metricQuery). | 
| `ONLY`               | If this is non-empty, only one test suite will be run and others will be skipped. Allowed values are `operator`, `podinfo`.                  |
| `E2E_DELETE_CLUSTER` | delete the k3d cluster after test finish (default `false`)                                                                                   |
