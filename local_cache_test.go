package lc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/allegro/bigcache/v3"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/smartystreets/goconvey/convey"
	"google.golang.org/protobuf/types/known/anypb"

	"trpc.group/trpc-go/trpc-go/codec"
)

type TestParam struct {
	Name   string
	Age    int
	ExInfo map[string]interface{}
	Any    *any.Any
}

// TestGetOrLoad 单测GetOrLoad
func TestGetOrLoad(t *testing.T) {
	convey.Convey("TestGetOrLoad", t, func() {
		convey.Convey("TestGetOrLoad run succ", func() {
			anyData, _ := anypb.New(&timestamp.Timestamp{Nanos: 1000, Seconds: 1000})
			setData := &TestParam{Name: "林延秋", Age: 24,
				ExInfo: map[string]interface{}{"Gender": true, "City": "shenzhen"},
				Any:    anyData}
			RegisterCache("test", WithLifeWindow(time.Second), WithCleanWindow(time.Second))
			cache := GetCache("test")

			key := "test-ok"
			err := cache.Set(key, setData, codec.SerializationTypeJSON)
			convey.So(err, convey.ShouldBeNil)

			getData := &TestParam{}
			err = cache.Get(key, getData, codec.SerializationTypeJSON)
			convey.So(err, convey.ShouldBeNil)
			convey.So(getData.Name, convey.ShouldEqual, setData.Name)

			<-time.After(3 * time.Second)
			err = cache.Get(key, getData, codec.SerializationTypeJSON)
			convey.So(err, convey.ShouldEqual, bigcache.ErrEntryNotFound)

			loadFunc := func() (interface{}, error) {
				return setData, nil
			}
			getData = &TestParam{}
			err = cache.GetWithLoad(context.TODO(), key, getData, loadFunc, codec.SerializationTypeJSON)
			convey.So(err, convey.ShouldBeNil)

			getData = &TestParam{}
			err = cache.Get(key, getData, codec.SerializationTypeJSON)
			convey.So(err, convey.ShouldBeNil)
		})

		convey.Convey("TestGetOrLoad run error", func() {
			anyData, _ := anypb.New(&timestamp.Timestamp{Nanos: 1000, Seconds: 1000})
			setData := &TestParam{Name: "林延秋", Age: 24,
				ExInfo: map[string]interface{}{"Gender": true, "City": "shenzhen"},
				Any:    anyData}
			RegisterCache("test", WithLifeWindow(time.Second), WithCleanWindow(1*time.Second),
				WithHardMaxCacheSize(128), WithMaxEntrySize(4096), WithMaxEntriesInWindow(2048*10),
				WithAllowUseExpiredEntry(true))
			cache := GetCache("test")

			loadFunc := func() (interface{}, error) {
				return setData, nil
			}
			key := "test-fail"
			getData := &TestParam{}
			err := cache.GetWithLoad(context.TODO(), key, getData, loadFunc, codec.SerializationTypeJSON)
			convey.So(err, convey.ShouldBeNil)
			convey.So(getData.Name, convey.ShouldEqual, setData.Name)

			getData = &TestParam{}
			status, err := cache.GetWithEntryStatus(key, getData, codec.SerializationTypeJSON)
			convey.So(err, convey.ShouldBeNil)
			convey.So(status, convey.ShouldEqual, bigcache.RemoveReason(0))

			// 数据过期&未清除
			time.Sleep(3 * time.Second)
			status, err = cache.GetWithEntryStatus(key, getData, codec.SerializationTypeJSON)
			convey.So(err, convey.ShouldNotBeNil)

			// cache过期，穿透失败，命中兜底
			getData = &TestParam{}
			err = cache.GetWithLoad(context.TODO(), key, getData, func() (interface{}, error) {
				return nil, errors.New("test-through-fail")
			}, codec.SerializationTypeJSON)
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(getData.Name, convey.ShouldNotEqual, setData.Name)
		})
	})
}
