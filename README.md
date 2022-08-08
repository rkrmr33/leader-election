# leader-election

Kubernetes native leader election pod that can be deployed as a sidecar to add leader-election as a service to any application that requires it, in a language agnostic way.

### Deployment as a sidecar container:
```
Add the following container to your deployment:

- name: leader-elector
  image: quay.io/roikramer120/leader-elector:v0.0.1
  command:
  - leader-elector
  args:
  - --id=$(POD_NAME)
  - --lease-name=$(LEASE_NAME)
  - --namespace=$(NAMESPACE)
  - --lease-duration=$(LEASE_DURATION)
  - --lease-renew-duration=$(LEASE_RENEW_DURATION)
  env:
  - name: NAMESPACE
  valueFrom:
      fieldRef:
      fieldPath: metadata.namespace
  - name: POD_NAME
  valueFrom:
      fieldRef:
      fieldPath: metadata.name
  - name: LEASE_NAME
  value: example
  - name: LEASE_DURATION
  value: 10s
  - name: LEASE_RENEW_DURATION
  value: 5s
  ports:
  - name: http # no need to expose this if it's a sidecar container
      containerPort: 4040
  securityContext:
  allowPrivilegeEscalation: false
  livenessProbe:
  httpGet:
      path: /healthz
      port: 4040
  initialDelaySeconds: 15
  periodSeconds: 20
  readinessProbe:
  httpGet:
      path: /readyz
      port: 4040
  initialDelaySeconds: 5
  periodSeconds: 10
  resources:
  limits:
      cpu: 200m
      memory: 200Mi
  requests:
      cpu: 100m
      memory: 100Mi
  imagePullPolicy: IfNotPresent
```
*You need to make sure that the pod service account is bound to a role with <b>at least</b> the following scopes:
```
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  labels:
    app.kubernetes.io/name: leader-elector
  name: leader-elector
rules:
  - apiGroups:
    - coordination.k8s.io
    resources:
    - leases
    verbs:
    - get
    - list
    - watch
    - create
    - update
    - patch
    - delete
  - apiGroups:
    - ""
    resources:
    - events
    verbs:
    - create
    - patch

```
