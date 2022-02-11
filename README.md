# kubectl-run

A Kubernetes addon for running an ephemeral container with a "mounted" volume.

Example:
```
mkdir -p ./stuff
echo "Test suite" > ./stuff/in.txt
kubectl ran busybox -e VAR1=Hello -e VAR2=world -v ./stuff:/stuff -- sh -c 'cp /stuff/in.txt && echo "$VAR1 $VAR2" >> /stuff/out.txt'
```

It works by
1. Starting a container with `sleep` in a new pod. You can optionally specify environment variables.
2. Copies any "mounted" volumes into the container.
3. Runs the specified command.
4. Copies the "mounted" volumes back out of the container.
5. Deletes the pod.

Requires the image contain `tail` and `tar`.

Limitations:
- Destination base name is currently ignored. Source will be copied into the dir name of the destination.
