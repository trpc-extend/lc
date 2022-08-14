package plugin

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"git.code.oa.com/trpc-go/trpc-go"
)

// KeyFunc 获取缓存key的方法 key: 指定数据缓存的key newRsp: 需要回包的实例对象
// (ServerFilter因为拿不到rsp实例对象需要用这个参数，ClientFilter可以忽略这个参数返回nil)
type KeyFunc func(ctx context.Context, req interface{}) (key string, newRsp interface{})

// DefaultKeyFunc 默认获取本地缓存key的方法
var DefaultKeyFunc KeyFunc = func(ctx context.Context, req interface{}) (string, interface{}) {
	body, _ := json.Marshal(req)
	h := md5.New()
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// keyFuncs key为rpcName
var keyFuncs = make(map[string]KeyFunc)

// RegisterKeyFunc 注册keyFunc
func RegisterKeyFunc(name string, fn KeyFunc) {
	keyFuncs[name] = fn
}

// GetKeyFunc 获取keyFunc
func GetKeyFunc(name string) KeyFunc {
	keyFunc, ok := keyFuncs[name]
	if ok {
		return keyFunc
	}
	return DefaultKeyFunc
}

// GetCacheFlag 获取是否命中缓存标志
func GetCacheFlag(ctx context.Context, cacheName string) string {
	return string(trpc.GetMetaData(ctx, fmt.Sprintf("%s_%s", pluginName, cacheName)))
}
