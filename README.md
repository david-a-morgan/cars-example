# Car Colored Controller

This is a minimal implementation to reproduce what I believe to be unintentional behavior in [controller-runtime](https://pkg.go.dev/sigs.k8s.io/controller-runtime).

This project was skaffolded out with [kubebuilder](https://book.kubebuilder.io/quick-start.html) and contains a single controller that reconciles Car custom resources. The reconcile logic is intentionally simple; create/update a ConfigMap with the labels from the owning Car object.

The believed unintended, and frankly unintuitive, behavior in controller-runtime relates to how events for owned objects are not filtered by any predicates that might be set on the controlled type.

## Background

controller-runtime contains a [builder package](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/builder) that allows, surprise, building a controller. Importantly, the controller can be associated with with one type to be reconciled (the [_For_](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/builder#Builder.For) type) and any number of types that are generated and owned by the _For_ type (the [_Owned_](https://github.com/kubernetes-sigs/controller-runtime/blob/v0.13.0/pkg/builder/controller.go#L106) types). The controller will receive a reconcile request for a _For_ object whenever one of its _Owned_ types is created/updated/deleted.

Additionally, both the _For_ type and the _Owned_ types can have predicates that will filter the create/update/delete events. Filtered events will not generate a reconcile request for the appropriate _For_ object.

## The Unexpected Behavior

Given a simple predicate:

```
prd, _ := predicate.LabelSelectorPredicate(metav1.LabelSelector{
    MatchLabels: map[string]string{"color": "red"},
})
```

We now associate that predicate to our _For_ type, which has one _Owned_ type:

```
ctrl.NewControllerManagedBy(mgr).
    For(&api.Car{}, builder.WithPredicates(prd)).
    Owns(&corev1.ConfigMap{}).
    Complete(r)
```

So with the above we have a controller that will manage `Car` objects that have the label `color` equal to `red`. If a user creates a `Car` object with the label `color` set to `blue` this controller won't receive any reconcile events for that new, blue `Car` object.

The unexpected occurs when the owned `ConfigMap` receives a create/update/delete event:

Let's assume we have two `Car` controllers; one for `Car` objects labeled `red` and one for `Car` objects labeled `blue`. A user creates a `Car` labeled `blue`. The red controller gets no reconcile request for the new, blue `Car` while the blue controller does receive a reconcile request. The blue controller then creates the owned `ConfigMap` (the blue `ConfigMap`) for the blue `Car`.

Now a new create event is published for the blue `ConfigMap`. As expected the blue controller translates that blue `ConfigMap` create event into a reconcile request for the blue `Car`. Unexpectedly, however, the red controller also translates that create event into a reconcile request, also for the blue `Car`. Where things blow up is that red controller generated reconcile request for a blue `Car` is handled by the red controller! Our red controller, which should handle **only** red `Car` objects is trying to handle a blue `Car`.

In this scenario the create/update/delete event for the blue `ConfigMap` ignores any predicates we have set for our _For_ type. At first blush this seems correct; we haven't specified any predicates for the _Owned_ type. However, because the event on the _Owned_ type gets translated to a reconcile request for the _For_ type this appears to be a way to bypass any filtering a controller expects for its _For_ type. It is unintutive that an event on an _Owned_ object can cause a controller that should **not** handle some _For_ objects to try to reconcile them.

## Running this minimal reproduction

This repo can be run with any local K8S cluster. I used Docker Desktop. If you're using a non-local K8S cluster make sure to push the built image to your Docker registry and update the image declarations in `manifest.yaml`.

1. `make docker-build`
1. `kubectl apply -f manifest.yaml`<br/>
    At this point you should have two controller pods running; one for red and one for blue. The controller logs will also note which color they are controlling.
1. `kubectl apply -f blue-car.yaml`<br/>
    Things will blow up now. The blue controller is fine:
    ```
    1.663698181430378e+09	INFO	reconciling car	{"controller": "car", "controllerGroup": "example.example.com", "controllerKind": "Car", "car": {"name":"car-blue","namespace":"cars-example-system"}, "namespace": "cars-example-system", "name": "car-blue", "reconcileID": "2bdc4863-1c90-452b-97ec-97e8b0ecee99", "car color": "blue", "controller color": "blue"}
    1.663698181447545e+09	INFO	reconciling car	{"controller": "car", "controllerGroup": "example.example.com", "controllerKind": "Car", "car": {"name":"car-blue","namespace":"cars-example-system"}, "namespace": "cars-example-system", "name": "car-blue", "reconcileID": "dd997990-0f18-4324-8fb2-1e143271e18d", "car color": "blue", "controller color": "blue"}
    ```
    The red controller, however, is not:
    ```
    1.6636981814459648e+09	INFO	reconciling car	{"controller": "car", "controllerGroup": "example.example.com", "controllerKind": "Car", "car": {"name":"car-blue","namespace":"cars-example-system"}, "namespace": "cars-example-system", "name": "car-blue", "reconcileID": "379900d7-2826-4d36-9bf9-cd26a7428bbd", "car color": "blue", "controller color": "red"}
    1.6636981814461052e+09	ERROR	!!!car color does not match controller color!!!	{"controller": "car", "controllerGroup": "example.example.com", "controllerKind": "Car", "car": {"name":"car-blue","namespace":"cars-example-system"}, "namespace": "cars-example-system", "name": "car-blue", "reconcileID": "379900d7-2826-4d36-9bf9-cd26a7428bbd"}
    sigs.k8s.io/controller-runtime/pkg/internal/controller.(*Controller).Reconcile
        /go/pkg/mod/sigs.k8s.io/controller-runtime@v0.12.2/pkg/internal/controller/controller.go:121
    sigs.k8s.io/controller-runtime/pkg/internal/controller.(*Controller).reconcileHandler
        /go/pkg/mod/sigs.k8s.io/controller-runtime@v0.12.2/pkg/internal/controller/controller.go:320
    sigs.k8s.io/controller-runtime/pkg/internal/controller.(*Controller).processNextWorkItem
        /go/pkg/mod/sigs.k8s.io/controller-runtime@v0.12.2/pkg/internal/controller/controller.go:273
    sigs.k8s.io/controller-runtime/pkg/internal/controller.(*Controller).Start.func2.2
        /go/pkg/mod/sigs.k8s.io/controller-runtime@v0.12.2/pkg/internal/controller/controller.go:234
    ```

So what's going on? We have two reconcile requests in the blue controller for the blue `Car`: one for the create of the `Car` itself and one for the create of the owned `ConfigMap`.

In the red controller we get one reconcile request: the create of the blue owned `ConfigMap`. The create event gets translated into a reconcile request for the blue `Car`.

## The Fix?

Ideally this would be handled in controller-runtime in a more intutive way. Events for _Owned_ objects will run the owner object through the _For_ predicates before publishing a reconcile request.

Alternatively you can set an additional predicate on each _Owned_ type. You'll need to write a predicate that gets the owner of the object that generated the event and check that owner object like the predicate used for the _For_ type. [controller-runtime/pkg/handler/EnqueueRequestForOwner](https://github.com/kubernetes-sigs/controller-runtime/blob/v0.13.0/pkg/handler/enqueue_owner.go#L119) shows a way to go from event to owner.

A quicker, dirtier way is just repeat your predicate logic in the reconcile method of your controller.
