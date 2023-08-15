package goconfig_center_nacos

import (
	"bytes"
	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	gocenter "github.com/nova2018/goconfig-center"
	"github.com/spf13/viper"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

type serverConfig struct {
	ContextPath string `json:"contextPath" mapstructure:"contextPath"`
	IpAddr      string `json:"ipAddr" mapstructure:"ipAddr"`
	Port        uint64 `json:"port" mapstructure:"port"`
	Scheme      string `json:"scheme" mapstructure:"scheme"`
	GrpcPort    uint64 `json:"grpcPort" mapstructure:"grpcPort"`
}

type clientConfig struct {
	NamespaceId          string `json:"namespaceId" mapstructure:"namespaceId"`
	Endpoint             string `json:"endpoint" mapstructure:"endpoint"`
	RegionId             string `json:"regionId" mapstructure:"regionId"`
	AccessKey            string `json:"accessKey" mapstructure:"accessKey"`
	SecretKey            string `json:"secretKey" mapstructure:"secretKey"`
	OpenKMS              bool   `json:"openKMS" mapstructure:"openKMS"`
	CacheDir             string `json:"cacheDir" mapstructure:"cacheDir"`
	UpdateThreadNum      int    `json:"updateThreadNum" mapstructure:"updateThreadNum"`
	NotLoadCacheAtStart  bool   `json:"notLoadCacheAtStart" mapstructure:"notLoadCacheAtStart"`
	UpdateCacheWhenEmpty bool   `json:"updateCacheWhenEmpty" mapstructure:"updateCacheWhenEmpty"`
	Username             string `json:"username" mapstructure:"username"`
	Password             string `json:"password" mapstructure:"password"`
	LogDir               string `json:"logDir" mapstructure:"logDir"`
	RotateTime           string `json:"rotateTime" mapstructure:"rotateTime"`
	MaxAge               int    `json:"maxAge" mapstructure:"maxAge"`
	LogLevel             string `json:"logLevel" mapstructure:"logLevel"`
}

type config struct {
	gocenter.ConfigDriver `mapstructure:",squash"`
	Prefix                string         `mapstructure:"prefix"`
	DataId                string         `mapstructure:"dataId"`
	Group                 string         `mapstructure:"group"`
	Type                  string         `mapstructure:"type"`
	Client                clientConfig   `mapstructure:"client"`
	Server                []serverConfig `mapstructure:"server"`
}

type driver struct {
	cfg        *config
	onChange   chan struct{}
	v          *viper.Viper
	dirty      bool
	closed     bool
	cLock      sync.RWMutex
	lock       sync.Mutex
	client     config_client.IConfigClient
	listDataId []string
	onClosed   chan struct{}
}

func (d *driver) Name() string {
	return d.cfg.Driver
}

func (d *driver) GetViper() (*viper.Viper, error) {
	if !d.dirty && d.v != nil {
		return d.v, nil
	}
	d.lock.Lock()
	defer d.lock.Unlock()
	v := viper.New()
	for _, dataId := range d.listDataId {
		configType := d.cfg.Type
		if configType == "" {
			configType = getConfigType(dataId)
		}
		content, err := d.client.GetConfig(vo.ConfigParam{
			DataId: dataId,
			Group:  d.cfg.Group,
		})
		if err != nil {
			return d.v, err
		}
		v.SetConfigType(configType)
		err = v.MergeConfig(bytes.NewBufferString(content))
		if err != nil {
			return v, err
		}
	}
	d.v = v
	d.dirty = false
	return v, nil
}

func getConfigType(dataId string) string {
	ext := filepath.Ext(dataId)

	if len(ext) > 1 {
		fileExt := ext[1:]
		// 还是要判断一下碰到，TEST.Namespace1
		// 会把Namespace1作为文件扩展名
		for _, e := range viper.SupportedExts {
			if e == fileExt {
				return fileExt
			}
		}
	}

	return "yaml"
}

func (d *driver) OnUpdate() <-chan struct{} {
	if d.onChange == nil && !d.closed {
		d.cLock.Lock()
		d.onChange = make(chan struct{})
		d.onClosed = make(chan struct{})
		d.cLock.Unlock()

		for _, dataId := range d.listDataId {
			dataId = strings.TrimSpace(dataId)
			_ = d.client.ListenConfig(vo.ConfigParam{
				DataId: dataId,
				Group:  d.cfg.Group,
				OnChange: func(namespace, group, dataId, data string) {
					// 广播通知
					d.cLock.RLock()
					if d.onChange != nil {
						d.lock.Lock()
						d.dirty = true
						d.lock.Unlock()
						d.onChange <- struct{}{}
					}
					d.cLock.RUnlock()
				},
			})
			go func(dataId string) {
				<-d.onClosed
				_ = d.client.CancelListenConfig(vo.ConfigParam{
					DataId: dataId,
					Group:  d.cfg.Group,
				})
			}(dataId)
		}
	}
	return d.onChange
}

func (d *driver) Close() error {
	if !d.closed {
		d.closed = true
		d.cLock.Lock()
		if d.onChange != nil {
			d.client.CloseClient()
			close(d.onChange)
			close(d.onClosed)
			d.onChange = nil
		}
		d.cLock.Unlock()
	}
	return nil
}

func (d *driver) Prefix() string {
	return d.cfg.Prefix
}

func factory(cfg *viper.Viper) (gocenter.Driver, error) {
	var c config
	if err := cfg.Unmarshal(&c); err != nil {
		return nil, err
	}

	client := buildClientConfig(c.Client)
	server := make([]constant.ServerConfig, 0, len(c.Server))
	for _, sc := range c.Server {
		server = append(server, buildServerConfig(sc))
	}

	configClient, err := clients.NewConfigClient(
		vo.NacosClientParam{
			ClientConfig:  &client,
			ServerConfigs: server,
		},
	)
	if err != nil {
		return nil, err
	}

	listDataId := strings.Split(c.DataId, ",")
	for k, v := range listDataId {
		listDataId[k] = strings.TrimSpace(v)
	}

	return &driver{
		cfg:        &c,
		client:     configClient,
		listDataId: listDataId,
	}, nil
}

func buildClientConfig(cfg clientConfig) constant.ClientConfig {
	c := &constant.ClientConfig{}
	_ = copyProperties(&cfg, c)
	return *c
}

func copyProperties(from, to interface{}) interface{} {
	reflectFrom, reflectTo := reflect.ValueOf(from), reflect.ValueOf(to)
	fields := reflectTo.Elem().NumField()
	for i := 0; i < fields; i++ {
		fieldName := reflectTo.Elem().Type().Field(i).Name
		origin := reflectFrom.Elem().FieldByName(fieldName)
		if origin.IsValid() && !origin.IsZero() {
			// 有效且非空
			if reflectTo.Elem().Field(i).CanSet() {
				reflectTo.Elem().Field(i).Set(origin)
			}
		}
	}
	return reflectTo.Interface()
}

func buildServerConfig(cfg serverConfig) constant.ServerConfig {
	c := &constant.ServerConfig{}
	_ = copyProperties(&cfg, c)
	return *c
}

func init() {
	gocenter.Register("nacos", factory)
}
