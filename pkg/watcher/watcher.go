package watcher

import (
	"context"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"time"
)

type channelSvcWatcher struct {
	sharedInformer cache.SharedIndexInformer
}

//Start used to implement the Runnable interface for manager support
func (sw channelSvcWatcher) Start(ctx context.Context) error {
	sw.sharedInformer.Run(ctx.Done())
	return nil
}

//NewChannelSvcWatcher is creating new "low level" informer for Services
//all events from this informer are piped into a channel which will be used to feed our controller
//with events from the hosted cluster
func NewChannelSvcWatcher(c *rest.Config, channel chan event.GenericEvent) channelSvcWatcher {
	sendMsg := func(obj interface{}) {
		channel <- event.GenericEvent{Object: obj.(client.Object)}
	}
	sendUpdateMsg := func(oldObj interface{}, newObj interface{}) {
		channel <- event.GenericEvent{Object: newObj.(client.Object)}
	}
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubernetes.NewForConfigOrDie(c), time.Second*300) //TODO make resync time an ENV
	si := kubeInformerFactory.Core().V1().Services().Informer()
	si.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sendMsg,
		DeleteFunc: sendMsg,
		UpdateFunc: sendUpdateMsg,
	})
	return channelSvcWatcher{sharedInformer: si}
}
