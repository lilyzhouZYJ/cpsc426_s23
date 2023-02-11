# B1

What do you think will happen if you delete the pod running your service? 
Note down your prediction, then run kubectl delete pod <pod-name> (you can 
find the name with kubectl get pod) and wait a few seconds, then run kubectl 
get pod again. What happened and why do you think so?

I think deleting the pod would lead the deployment to create a new pod. This is 
because we have described the desired state of the deployment as having one pod,
so if the one pod is deleted, the Deployment Controller will change the state to
the desired state, hence starting a new pod.

After running `kubectl delete pod <pod-name>` and then running `kubectl get pod`,
I noted that a new pod has been created. Running `kubectl get deployments` also
reflects this, because the READY field is 1/1.
