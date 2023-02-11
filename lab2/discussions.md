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



# B4

Result from https://lab2.cs426.cloud/recommend/yz878/video-rec/yz878/:

Welcome! You have chosen user ID 204095 (DuBuque1963/genevievewisoky@kuhlman.net)

Their recommended videos are:
 1. wild Radicchio by Alden Ward
 2. DarkSeaGreendesk: bypass by Hubert Mraz
 3. Handstand: index by Clementine Weber
 4. attractive Okra by Katlyn Buckridge
 5. The repelling skunk's calm by Dennis Cummerata



# C3

What does the load distribution look like with a client pool size of 4? 
What would you expect to happen if you used 1 client? How about 8? 

With 1 client, there is no load-balancing between the pods, i.e. all requests to 
UserService would get sent to the same pod, and all requests to VideoService get
sent to the same pod. Hence, all the load would land on the same pod.

With both 4 and 8 clients, there would be load balancing. The load would thus be
distributed between the two pods (for both UserService and VideoService).

TODO: differentiate between 4 and 8?

Is 8 more evenly distributed? Not necessarily because each connection always gets
sent to the same pod?