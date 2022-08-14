package lc

import (
	"time"

	"github.com/allegro/bigcache/v3"
)

// Config for BigCache
type Config struct {
	// Number of cache shards, value must be a power of two
	Shards int `yaml:"shards"`
	// Time after which entry can be evicted
	LifeWindow time.Duration `yaml:"life_window"`
	// Interval between removing expired entries (clean up).
	// If set to <= 0 then no action is performed.
	// Setting to < 1 second is counterproductive — bigcache has a one second resolution.
	CleanWindow time.Duration `yaml:"clean_window"`
	// Max number of entries in life window. Used only to calculate initial size for cache shards.
	// When proper value is set then additional memory allocation does not occur.
	MaxEntriesInWindow int `yaml:"max_entries_in_window"`
	// Max size of entry in bytes. Used only to calculate initial size for cache shards.
	MaxEntrySize int `yaml:"max_entry_size"`
	// StatsEnabled if true calculate the number of times a cached resource was requested.
	StatsEnabled bool `yaml:"stats_enabled"`
	// Verbose mode prints information about new memory allocation
	Verbose bool `yaml:"verbose"`
	// HardMaxCacheSize is a limit for cache size in MB. Cache will not allocate more memory than this limit.
	// It can protect application from consuming all available memory on machine, therefore from running OOM Killer.
	// Default value is 0 which means unlimited size. When the limit is higher than 0 and reached then
	// the oldest entries are overridden for the new ones.
	HardMaxCacheSize int `yaml:"hard_max_cache_size"`
	// OnRemove is a callback fired when the oldest entry is removed because of its expiration time or no space left
	// for the new entry, or because delete was called.
	// Default value is nil which means no callback and it prevents from unwrapping the oldest entry.
	// ignored if OnRemoveWithMetadata is specified.
	OnRemove func(key string, entry []byte)
	// OnRemoveWithMetadata is a callback fired
	// when the oldest entry is removed because of its expiration time or no space left
	// for the new entry, or because delete was called. A structure representing details about that specific entry.
	// Default value is nil which means no callback and it prevents from unwrapping the oldest entry.
	OnRemoveWithMetadata func(key string, entry []byte, keyMetadata bigcache.Metadata)
	// OnRemoveWithReason is a callback fired when the oldest entry is removed
	// because of its expiration time or no space left
	// for the new entry, or because delete was called. A constant representing the reason will be passed through.
	// Default value is nil which means no callback and it prevents from unwrapping the oldest entry.
	// Ignored if OnRemove is specified.
	OnRemoveWithReason func(key string, entry []byte, reason bigcache.RemoveReason)
	// Logger is a logging interface and used in combination with `Verbose`
	// Defaults to `DefaultLogger()`
	Logger bigcache.Logger

	// AllowUseExpiredEntry 允许使用未清除数据
	AllowUseExpiredEntry bool `yaml:"allow_use_expired_entry"`
}

// Option 声明cache的option
type Option func(*Config)

// WithShards 设置shards个数
func WithShards(i int) Option {
	return func(c *Config) {
		c.Shards = i
	}
}

// WithLifeWindow 设置生命周期
func WithLifeWindow(t time.Duration) Option {
	return func(c *Config) {
		c.LifeWindow = t
	}
}

// WithCleanWindow 设置清理周期
func WithCleanWindow(t time.Duration) Option {
	return func(c *Config) {
		c.CleanWindow = t
	}
}

// WithMaxEntriesInWindow 设置最大数据条数，仅初始化使用，影响使用的最小内存
func WithMaxEntriesInWindow(i int) Option {
	return func(c *Config) {
		c.MaxEntriesInWindow = i
	}
}

// WithMaxEntrySize 设置最大value字节数，仅初始化使用，影响使用的最小内存
func WithMaxEntrySize(i int) Option {
	return func(c *Config) {
		c.MaxEntrySize = i
	}
}

// WithLogger 日志类型
func WithLogger(l bigcache.Logger) Option {
	return func(c *Config) {
		c.Logger = l
	}
}

// WithHardMaxCacheSize 最大cache大小，单位MB，防止申请内存过多导致OOM，高于0起作用
func WithHardMaxCacheSize(m int) Option {
	return func(c *Config) {
		c.HardMaxCacheSize = m
	}
}

// WithVerbose 开启后输出内存申请信息
func WithVerbose(v bool) Option {
	return func(c *Config) {
		c.Verbose = v
	}
}

// WithStatsEnabled 开启后计算单个key的命中次数
func WithStatsEnabled(s bool) Option {
	return func(c *Config) {
		c.StatsEnabled = s
	}
}

// WithAllowUseExpiredEntry 允许使用未清除的entry
func WithAllowUseExpiredEntry(allowed bool) Option {
	return func(c *Config) {
		c.AllowUseExpiredEntry = allowed
	}
}
