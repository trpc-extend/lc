package lc

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/allegro/bigcache/v3"
	"golang.org/x/sync/singleflight"

	"git.code.oa.com/trpc-go/trpc-go/codec"
	"git.code.oa.com/trpc-go/trpc-go/errs"
	"git.code.oa.com/trpc-go/trpc-go/log"
)

var (
	// ErrTypeNotEqual 类型不匹配
	ErrTypeNotEqual = errs.New(2001, "lc: type not equal")
	// ErrNotCanSet interface不允许set
	ErrNotCanSet = errs.New(2002, "lc: value cannot set")
)

// Cache 缓存对象
type Cache struct {
	name                 string
	bc                   *bigcache.BigCache
	group                *singleflight.Group
	allowUseExpiredEntry bool // 是否降级
}

var (
	cachePool = struct {
		sync.RWMutex
		m map[string]*Cache
	}{m: make(map[string]*Cache)}
)

// RegisterCache 注册cache对象 提前注册好处如果设置参数不合适会发生panic
func RegisterCache(name string, opts ...Option) {
	newCache := createCache(name, opts...)
	cachePool.Lock()
	cachePool.m[name] = newCache
	cachePool.Unlock()
}

// GetCache 获取cache对象 如果没有则创建一个默认cache对象
func GetCache(name string) *Cache {
	cachePool.RLock()
	if cache, ok := cachePool.m[name]; ok {
		cachePool.RUnlock()
		return cache
	}
	cachePool.RUnlock()
	// 如果没有创建一个默认cache对象
	newCache := createCache(name)
	cachePool.Lock()
	cachePool.m[name] = newCache
	cachePool.Unlock()
	return newCache
}

// createCache 创建cache
func createCache(name string, opts ...Option) *Cache {
	cfg := &Config{
		Shards:             128,              // 默认128分片
		LifeWindow:         60 * time.Second, // 默认60秒生命周期
		CleanWindow:        30 * time.Second, // 默认30秒清除窗口周期
		MaxEntriesInWindow: 1000 * 10 * 60,   // 在最大窗口期最大缓存数目
		MaxEntrySize:       1024,             // 默认每个司令大小 字节
		StatsEnabled:       true,
		Verbose:            true,
		HardMaxCacheSize:   2046, // 默认最大硬件内存2046M
		Logger:             bigcache.DefaultLogger(),
	}
	for _, opt := range opts {
		opt(cfg)
	}
	bc, err := bigcache.NewBigCache(buildBigcacheConfig(cfg))
	if err != nil {
		panic(err)
	}
	go func() {
		ticker := time.NewTicker(3 * time.Minute) // 3分钟上报一次
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// length：数据条数, capacity:占用容量, stats:统计
				log.Warnf("lc stats name:%v, length:%v, capacity:%vMB, stats:%+v", name, bc.Len(),
					bc.Capacity()/(1024*1024), bc.Stats())
			}
		}
	}()
	log.Warnf("new local cache, name:%v, length:%v, capacity:%vMB", name, bc.Len(), bc.Capacity()/(1024*1024))
	return &Cache{
		name:                 name,
		bc:                   bc,
		group:                &singleflight.Group{},
		allowUseExpiredEntry: cfg.AllowUseExpiredEntry,
	}
}

func buildBigcacheConfig(cfg *Config) bigcache.Config {
	return bigcache.Config{
		Shards:             cfg.Shards,
		LifeWindow:         cfg.LifeWindow,
		CleanWindow:        cfg.CleanWindow,
		MaxEntriesInWindow: cfg.MaxEntriesInWindow,
		MaxEntrySize:       cfg.MaxEntrySize,
		StatsEnabled:       cfg.StatsEnabled,
		Verbose:            cfg.Verbose,
		HardMaxCacheSize:   cfg.HardMaxCacheSize,
		Logger:             cfg.Logger,
	}
}

// Close 会发出关闭信号，退出clean协程，保证可以被gc
func (c *Cache) Close() error {
	return c.bc.Close()
}

// GetBytes 获取key对应的原始二进制值，不存在返回ErrEntryNotFound
func (c *Cache) GetBytes(key string) ([]byte, error) {
	return c.bc.Get(key)
}

// Get 获取key对应的值，不存在返回ErrEntryNotFound, 通过输入序列化方式自动解析数据结构
func (c *Cache) Get(key string, val interface{}, serializationType int) error {
	entry, err := c.bc.Get(key)
	if entry == nil || err != nil {
		return err
	}
	err = codec.Unmarshal(serializationType, entry, val)
	if err != nil {
		return err
	}
	return nil
}

// GetWithEntryStatus 获取val以及entry status
func (c *Cache) GetWithEntryStatus(key string, val interface{}, serializationType ...int) (
	bigcache.RemoveReason, error) {
	entry, rsp, err := c.bc.GetWithInfo(key)
	if err != nil {
		return bigcache.RemoveReason(0), err
	}
	if len(serializationType) > 0 {
		if err = codec.Unmarshal(serializationType[0], entry, val); err != nil {
			return bigcache.RemoveReason(0), err
		}
	}
	return rsp.EntryStatus, nil
}

// LoadFunc 加载数据函数
type LoadFunc func() (interface{}, error)

// GetOrLoad Get或通过fn重新load数据
// load func加singleflight做并发控制
// fn 缓存失效透传函数
// serializationType 存储value的序列化方式
func (c *Cache) GetOrLoad(ctx context.Context, key string, value interface{}, fn LoadFunc,
	serializationType ...int) error {
	entryStatus, err := c.GetWithEntryStatus(key, value, serializationType...)
	if err == nil && entryStatus == bigcache.RemoveReason(0) {
		return nil
	}
	expiredButNotClean := false
	// 数据过期， 但是未清除
	if err == nil && entryStatus == bigcache.Expired {
		expiredButNotClean = true
	}
	// value不能Set 直接返回错误
	if !reflect.ValueOf(value).Elem().CanSet() {
		return ErrNotCanSet
	}
	// 数据穿透, singleflight call
	rsp, err, _ := c.group.Do(key, func() (interface{}, error) {
		newValue, fnErr := fn() // 执行穿透的func
		// 从兜底cache中拉取
		if fnErr != nil || newValue == nil {
			// 穿透函数执行失败，留个日志
			log.ErrorContextf(ctx, "lc through fail, err: %v, new value: %+v", fnErr, newValue)
			return newValue, fnErr
		}
		log.DebugContextf(ctx, "lc through success, key:%v, new value: %+v", key, newValue)
		// 写cache
		setErr := c.Set(key, newValue, serializationType...)
		if setErr != nil {
			log.ErrorContextf(ctx, "lc: set entry err: %v, key: %v", setErr, key)
		}
		return newValue, fnErr
	})
	// 透传请求失败 && 数据过期未清除 && 允许使用过期entry
	if err != nil && expiredButNotClean && c.allowUseExpiredEntry {
		log.ErrorContextf(ctx, "lc: use expired but not clean entry, key:%v, value:%v", key, value)
		return nil
	}
	if err != nil { // 数据过期并且已清除
		return err
	}
	// 类型必须一致
	if reflect.TypeOf(rsp).Kind() != reflect.TypeOf(value).Kind() {
		return ErrTypeNotEqual
	}
	// copy
	reflect.ValueOf(value).Elem().Set(reflect.ValueOf(rsp).Elem())
	return nil
}

// Set 保存一对<key, value>，可能因value格式不支持而保存失败, 通过输入序列化方式自动打包数据
func (c *Cache) Set(key string, val interface{}, serializationType ...int) error {
	entry, err := marshal(val, serializationType...)
	if err != nil {
		return err
	}
	return c.bc.Set(key, entry)
}

// 序列化
func marshal(val interface{}, serializationType ...int) ([]byte, error) {
	if len(serializationType) > 0 {
		return codec.Marshal(serializationType[0], val)
	}
	var err error
	var buf []byte
	switch val.(type) {
	case []byte:
		buf = val.([]byte)
	case string:
		buf = []byte(val.(string))
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool, error:
		if b, e := ToString(val); e != nil {
			return buf, errs.Newf(2003, "lc: val type:%s ToString error:%s", reflect.TypeOf(val), e.Error())
		} else {
			buf = []byte(b)
		}
	default:
		err = errs.Newf(2004, "lc: value not support type:%s", reflect.TypeOf(val))
	}
	return buf, err
}

// Delete 删除一个key
func (c *Cache) Delete(key string) error {
	return c.bc.Delete(key)
}

// Reset 清空所有cache的shard
func (c *Cache) Reset() error {
	return c.bc.Reset()
}

// Len 返回cache中的数据条数
func (c *Cache) Len() int {
	return c.bc.Len()
}

// Capacity 返回cache中的已经使用字节数
func (c *Cache) Capacity() int {
	return c.bc.Capacity()
}

// Stats 返回cache命中的统计数据
func (c *Cache) Stats() bigcache.Stats {
	return c.bc.Stats()
}

// Iterator 返回一个可遍历整个cache的迭代器
func (c *Cache) Iterator() *bigcache.EntryInfoIterator {
	return c.bc.Iterator()
}
