package goconfig_center_nacos

import (
	"bytes"
	"fmt"
	"github.com/nova2018/goconfig"
	gocenter "github.com/nova2018/goconfig-center"
	"github.com/spf13/viper"
	"regexp"
	"testing"
	"time"
)

func TestNacos(t *testing.T) {
	toml := []byte(`
[config_center]
enable = true
[[config_center.drivers]]
enable = true
driver = "nacos"
dataId = "test.yaml,uaa-service-dev.yaml"
group = "DEFAULT_GROUP"
[config_center.drivers.client]
logLevel = "debug"
logDir = "./"
timeoutMs = 500000
cacheDir = "./"
[[config_center.drivers.server]]
ipAddr = "127.0.0.1"
contextPath = "/nacos"
scheme = "http"
port = 8848
`)

	v := viper.New()
	v.SetConfigType("toml")
	_ = v.ReadConfig(bytes.NewBuffer(toml))

	fmt.Println("v.AllSettings:", v.AllSettings())
	fmt.Println("v.AllKeys:", v.AllKeys())

	//gconfig := goconfig.New()
	//gconfig.AddNoWatchViper(v)
	//gg := gocenter.New(gconfig)
	//gg.Watch()
	//gconfig.StartWait()

	center := gocenter.NewWithViper(v)
	center.Watch()
	gconfig := center.GetConfig()

	gconfig.OnKeyChange("xxx", func() {
		fmt.Println("key update!!!")
	})

	gconfig.OnMapKeyChange("abc", func(e goconfig.ConfigUpdateEvent) {
		fmt.Println(e)
	})

	mKey, _ := regexp.Compile(`^x\.1\.abc`)
	gconfig.OnMatchKeyChange(mKey, func(e goconfig.ConfigUpdateEvent) {
		fmt.Println(e)
	})

	for {
		time.Sleep(1 * time.Second)
		fmt.Printf("%v 1app.AllSettings:%v\n", time.Now(), gconfig.GetConfig().AllSettings())
	}
}
