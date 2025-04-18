package nacos

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/go-kratos/kratos/v2/registry"
	"github.com/dbsyk/nacos-sdk-go/v2/clients/naming_client"
	"github.com/dbsyk/nacos-sdk-go/v2/common/constant"
	"github.com/dbsyk/nacos-sdk-go/v2/vo"
)

var ErrServiceInstanceNameEmpty = errors.New("kratos/nacos: ServiceInstance.Name can not be empty")

var (
	_ registry.Registrar = (*Registry)(nil)
	_ registry.Discovery = (*Registry)(nil)
)

type options struct {
	prefix  string
	weight  float64
	cluster string
	group   string
	kind    string
}

type Option func(o *options)

func WithPrefix(prefix string) Option {
	return func(o *options) { o.prefix = prefix }
}

func WithWeight(weight float64) Option {
	return func(o *options) { o.weight = weight }
}

func WithCluster(cluster string) Option {
	return func(o *options) { o.cluster = cluster }
}

func WithGroup(group string) Option {
	return func(o *options) { o.group = group }
}

func WithDefaultKind(kind string) Option {
	return func(o *options) { o.kind = kind }
}

type Registry struct {
	opts options
	cli  naming_client.INamingClient
}

func New(cli naming_client.INamingClient, opts ...Option) *Registry {
	op := options{
		prefix:  "/microservices",
		cluster: "DEFAULT",
		group:   constant.DEFAULT_GROUP,
		weight:  100,
		kind:    "grpc",
	}
	for _, option := range opts {
		option(&op)
	}
	return &Registry{
		opts: op,
		cli:  cli,
	}
}

func (r *Registry) Register(_ context.Context, si *registry.ServiceInstance) error {
	if si.Name == "" {
		return ErrServiceInstanceNameEmpty
	}
	for _, endpoint := range si.Endpoints {
		u, err := url.Parse(endpoint)
		if err != nil {
			return err
		}
		host, port, err := net.SplitHostPort(u.Host)
		if err != nil {
			return err
		}
		p, err := strconv.Atoi(port)
		if err != nil {
			return err
		}
		meta := map[string]string{"kind": u.Scheme, "version": si.Version}
		for k, v := range si.Metadata {
			meta[k] = v
		}
		_, err = r.cli.RegisterInstance(vo.RegisterInstanceParam{
			Ip:          host,
			Port:        uint64(p),
			ServiceName: si.Name + "." + u.Scheme,
			Weight:      r.opts.weight,
			Enable:      true,
			Healthy:     true,
			Ephemeral:   true,
			Metadata:    meta,
			ClusterName: r.opts.cluster,
			GroupName:   r.opts.group,
		})
		if err != nil {
			return fmt.Errorf("RegisterInstance err: %v, %v", err, endpoint)
		}
	}
	return nil
}

func (r *Registry) Deregister(_ context.Context, service *registry.ServiceInstance) error {
	for _, endpoint := range service.Endpoints {
		u, err := url.Parse(endpoint)
		if err != nil {
			return err
		}
		host, port, err := net.SplitHostPort(u.Host)
		if err != nil {
			return err
		}
		p, err := strconv.Atoi(port)
		if err != nil {
			return err
		}
		_, err = r.cli.DeregisterInstance(vo.DeregisterInstanceParam{
			Ip:          host,
			Port:        uint64(p),
			ServiceName: service.Name + "." + u.Scheme,
			GroupName:   r.opts.group,
			Cluster:     r.opts.cluster,
			Ephemeral:   true,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) Watch(ctx context.Context, serviceName string) (registry.Watcher, error) {
	return newWatcher(ctx, r.cli, serviceName, r.opts.group, r.opts.kind, []string{r.opts.cluster})
}

func (r *Registry) GetService(_ context.Context, serviceName string) ([]*registry.ServiceInstance, error) {
	res, err := r.cli.SelectInstances(vo.SelectInstancesParam{
		ServiceName: serviceName,
		GroupName:   r.opts.group,
		HealthyOnly: true,
	})
	if err != nil {
		return nil, err
	}
	var items []*registry.ServiceInstance
	for _, in := range res {
		kind := r.opts.kind
		if k, ok := in.Metadata["kind"]; ok {
			kind = k
		}
		items = append(items, &registry.ServiceInstance{
			ID:        in.InstanceId,
			Name:      in.ServiceName,
			Version:   in.Metadata["version"],
			Metadata:  in.Metadata,
			Endpoints: []string{fmt.Sprintf("%s://%s:%d", kind, in.Ip, in.Port)},
		})
	}
	return items, nil
}
