# kubectl-ran

A Kubernetes addon for running an ephemeral container with a "mounted" volume.

Example:
```
mkdir -p ./stuff
echo "Test suite" > ./stuff/in.txt
kubectl ran busybox -e VAR1=Hello -e VAR2=world -v ./stuff:/stuff -- sh -c 'cp /stuff/in.txt /stuff/out.txt && echo "$VAR1 $VAR2" >> /stuff/out.txt'
cat ./stuff/out.txt
```

It works by
1. Starting a container with `tail -f /dev/null` in a new pod. You can optionally specify environment variables.
2. Copies any "mounted" volumes into the container.
3. Runs the specified command.
4. Copies the "mounted" volumes back out of the container.
5. Deletes the pod.

Requires the image contain `tail` and `tar`.

You can provide a file with `--pod-file pod.yaml` to specify additional options for the pod or add a sidecar, as in
```
spec:
  containers:
  - name: selenium
    image: selenium/standalone-chrome
    ports:
    - name: selenium
      containerPort: 4444
    readinessProbe:
      httpGet:
        path: /
        port: 4444
    volumeMounts:
    - mountPath: /dev/shm
      name: dshm
  volumes:
  - name: dshm
    emptyDir:
    medium: Memory
```
