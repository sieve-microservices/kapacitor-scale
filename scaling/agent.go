package scaling

import (
	"acos.alcatel-lucent.com/scmrepos/git/micro-analytics/kapacitor-scaling/rancher"
	"fmt"
	"sync"
	"time"
)

type Service struct {
	sync.RWMutex
	CurrentInstances int64
	Name, Id         string
	CooldownUntil    time.Time
}

type Agent struct {
	sync.RWMutex
	serviceMap map[string]*Service
	client     rancher.Client
}

func New(client rancher.Client) *Agent {
	a := &Agent{}
	a.serviceMap = make(map[string]*Service)
	a.client = client
	return a
}

func (a *Agent) get(serviceId string) *Service {
	a.RLock()
	v, ok := a.serviceMap[serviceId]
	if ok {
		a.RUnlock()
		return v
	}
	a.RUnlock()
	a.Lock()
	defer a.Unlock()
	// FIXME memory leak because services are never freed up
	v, ok = a.serviceMap[serviceId]
	if !ok {
		a.serviceMap[serviceId] = &Service{Id: serviceId}
	}
	return a.serviceMap[serviceId]
}

// caller must call unlock service!
func (a *Agent) RequestScaling(serviceId string, eventTime time.Time) (*Service, error) {
	s := a.get(serviceId)
	s.RLock()
	if s.CooldownUntil.Sub(eventTime) > 0 {
		s.RUnlock()
		return nil, nil
	}
	s.RUnlock()
	s.Lock()
	if s.CooldownUntil.Sub(eventTime) > 0 {
		s.Unlock()
		return nil, nil
	}
	u := "v1/services/" + serviceId
	rancherService := rancher.Service{}
	if err := a.client.Get(u, &rancherService); err != nil {
		s.Unlock()
		return nil, fmt.Errorf("Could not get scale count of service %s: %s", serviceId, err)
	}
	// Backoff strategy to save requests?
	if rancherService.Transitioning != "no" {
		s.Unlock()
		return nil, nil
	}
	s.CurrentInstances = rancherService.Scale
	s.Name = rancherService.Name
	return s, nil
}

func (a *Agent) Scale(serviceId string, count int64) error {
	data := map[string]int64{"scale": count}
	if err := a.client.Put("v1/services/"+serviceId, data, nil); err != nil {
		return fmt.Errorf("Failed to scale up service: %s", err)
	}
	return nil
}
