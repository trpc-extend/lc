package plugin

import (
	"context"
	"fmt"
	"strconv"
	"time"

	pcgmonitor "git.code.oa.com/pcgmonitor/trpc_report_api_go"
	"git.code.oa.com/pcgmonitor/trpc_report_api_go/pb/nmnt"
	"git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/codec"
	"git.code.oa.com/trpc-go/trpc-go/errs"
	"git.code.oa.com/trpc-go/trpc-go/filter"
	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.code.oa.com/trpc-go/trpc-go/plugin"
	"git.woa.com/trpc-extend/trpc-go/lc"
)

const (
	pluginName = "rpc_cache"
	pluginType = "cache"

	ForbidCacheFlag = "forbid_cache" // 禁止使用缓存
	HitCacheFlag    = "hit_cache"    // 命中缓存
	NoHitCacheFlag  = "no_hit_cache" // 未命中缓存
)

func init() {
	plugin.Register(pluginName, &RpcCachePlugin{})
}

// Cache 缓存对象
type Cache struct {
	// CacheName cache名称 用于监控上报
	CacheName string `yaml:"cache_name"`
	// RPCName rpc名称
	RPCName string `yaml:"rpc_name"`
	// Shards 分区数
	Shards int `yaml:"shards"`
	// LifeWindow entry生命周期，对应bigcache LifeWindow配置
	LifeWindow int64 `yaml:"life_window"`
	// CleanWindow 数据清理时间间隔，对应bigcache CleanWindow配置
	CleanWindow int64 `yaml:"clean_window"`
	// HardMaxCacheSize cache最大占用内容, 单位MB
	HardMaxCacheSize int `yaml:"hard_max_cache_size"`
	// SerializationType 序列化类型:
	SerializationType int `yaml:"serialization_type"`
	// MaxEntriesInWindow 初始化时申请内存时使用
	MaxEntriesInWindow int `yaml:"max_entries_in_window"`
	// MaxEntrySize 单位byte，初始化内存时使用
	MaxEntrySize int `yaml:"max_entry_size"`
	// AllowUseExpiredEntry 在请求失败的情况下，允许使用未清除数据兜底
	AllowUseExpiredEntry bool `yaml:"allow_use_expired_entry"`

	// FailoverRedis 兜底的redis配置 TODO 待支持redis兜底
	FailoverRedis string `yaml:"failover_redis"`
	// FailoverEviction 兜底redis key的过期时间, 默认3天过期
	FailoverEviction uint64 `yaml:"failover_eviction"`
	// cache对象 根据rpc生成不同的cache
	lc *lc.Cache `yaml:"-"`
}

// RpcCachePlugin 本地Cache插件
type RpcCachePlugin struct {
	caches map[string]Cache
}

// Type 插件类型名称
func (t *RpcCachePlugin) Type() string {
	return pluginType
}

// Setup 装载reqcache拦截器实现
func (t *RpcCachePlugin) Setup(name string, decoder plugin.Decoder) error {
	caches := make([]Cache, 0)
	if err := decoder.Decode(&caches); err != nil {
		log.Errorf("RpcCachePlugin Decode Failed! name:%v, err:%v", name, err)
		return err
	}
	log.Infof("RpcCachePlugin Setup name:%v, caches:%+v", name, caches)
	t.caches = make(map[string]Cache)
	for _, c := range caches {
		lc.RegisterCache(c.RPCName,
			lc.WithShards(c.Shards),
			lc.WithLifeWindow(time.Duration(c.LifeWindow)*time.Second),
			lc.WithCleanWindow(time.Duration(c.CleanWindow)*time.Second),
			lc.WithHardMaxCacheSize(c.HardMaxCacheSize),
			lc.WithVerbose(true),
			lc.WithMaxEntriesInWindow(c.MaxEntriesInWindow),
			lc.WithMaxEntrySize(c.MaxEntrySize))
		c.lc = lc.GetCache(c.RPCName)
		t.caches[c.RPCName] = c
	}
	filter.Register(name, ServerFilter(t), ClientFilter(t))
	return nil
}

// ServerFilter rpc_cache服务端拦截器
func ServerFilter(t *RpcCachePlugin) filter.ServerFilter {
	return func(ctx context.Context, req interface{}, handle filter.ServerHandleFunc) (interface{}, error) {
		rpcName := codec.Message(ctx).ServerRPCName()
		if v, ok := t.caches[rpcName]; ok && v.lc != nil {
			keyFunc := GetKeyFunc(rpcName)
			key, newRsp := keyFunc(ctx, req)
			if key != "" && newRsp != nil {
				// 命中缓存配置策略
				hitCacheFlag := HitCacheFlag
				loadFunc := func() (interface{}, error) {
					hitCacheFlag = NoHitCacheFlag
					subRsp, subErr := handle(ctx, req)
					return subRsp, subErr
				}
				err := v.lc.GetOrLoad(ctx, key, newRsp, loadFunc, v.SerializationType)
				trpc.SetMetaData(ctx, fmt.Sprintf("%s_%s", pluginName, v.CacheName), []byte(hitCacheFlag))
				reportCacheMonitor(v.CacheName, rpcName, hitCacheFlag, err)
				return newRsp, err
			} else {
				// 返回的key或者newRsp不可用
				reportCacheMonitor(v.CacheName, rpcName, ForbidCacheFlag, nil)
				trpc.SetMetaData(ctx, fmt.Sprintf("%s_%s", pluginName, v.CacheName), []byte(ForbidCacheFlag))
				return handle(ctx, req)
			}
		} else {
			// 如果接口没有配置
			return handle(ctx, req)
		}
	}
}

// ClientFilter rpc_cache客户端拦截器
func ClientFilter(t *RpcCachePlugin) filter.ClientFilter {
	return func(ctx context.Context, req interface{}, rsp interface{}, handle filter.ClientHandleFunc) error {
		rpcName := codec.Message(ctx).ClientRPCName()
		if v, ok := t.caches[rpcName]; ok && v.lc != nil {
			keyFunc := GetKeyFunc(rpcName)
			key, _ := keyFunc(ctx, req)
			if key != "" && rsp != nil {
				// 命中缓存配置策略
				hitCacheFlag := HitCacheFlag
				loadFunc := func() (interface{}, error) {
					hitCacheFlag = NoHitCacheFlag
					err := handle(ctx, req, rsp)
					return rsp, err
				}
				err := v.lc.GetOrLoad(ctx, key, rsp, loadFunc, v.SerializationType)
				trpc.SetMetaData(ctx, fmt.Sprintf("%s_%s", pluginName, v.CacheName), []byte(hitCacheFlag))
				reportCacheMonitor(v.CacheName, rpcName, hitCacheFlag, err)
				return err
			} else {
				// 返回的key或者rsp不可用
				reportCacheMonitor(v.CacheName, rpcName, ForbidCacheFlag, nil)
				trpc.SetMetaData(ctx, fmt.Sprintf("%s_%s", pluginName, v.CacheName), []byte(ForbidCacheFlag))
				return handle(ctx, req, rsp)
			}
		} else {
			// 如果接口没有配置
			return handle(ctx, req, rsp)
		}
	}
}

// reportCacheMonitor 上报缓存监控数据
func reportCacheMonitor(cacheName, rpcName, hitCacheFlag string, err error) {
	key := []string{cacheName, rpcName, hitCacheFlag, strconv.Itoa(errs.Code(err)), ""}
	hit := int64(0)
	miss := int64(0)
	other := int64(0)
	fail := int64(0)
	if err != nil {
		fail = 1
	} else {
		if hitCacheFlag == HitCacheFlag {
			hit = 1
		} else if hitCacheFlag == NoHitCacheFlag {
			miss = 1
		} else {
			other = 1
		}
	}

	values := []int64{
		1,     // 上报总数
		hit,   // 命中缓存
		miss,  // 丢失缓存
		other, // 禁止缓存
		fail,  // 异常失败
		0,     // 保留1
	}
	stat := make([]*nmnt.StatValue, len(values))
	for i, v := range values {
		stat[i] = &nmnt.StatValue{Value: float64(v), Count: 1, Policy: nmnt.Policy_SUM}
	}
	if err = pcgmonitor.ReportCustom(pluginName, key, stat); err != nil {
		log.Errorf("pcgmonitor ReportCustom failed! err: %v", err)
	}
}
