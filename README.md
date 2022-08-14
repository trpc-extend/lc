# rpc_cache 服务端拦截器
- 基于bigcache和singleflight实现
1. 接口级缓存
2. 缓存穿透控制
3. 兜底能力：基于本地cache实现，重启后失效

trpc-go框架配置：
```
server:     
  filter:
    - rpc_cache # 插件名称

plugins:
 cache: # plugin type
     rpc_cache: # plugin name
       - cache_name: "GetIntroduction" #标识缓存名称, 可用于数据监控上报
         rpc_name: "/trpc.video_detail.sport_national_group.NationalGroupService/GetIntroduction"
         # 以下配置项对应bigcache配置
         life_window: 60 #缓存时间, 单位s
         clean_window: 120 #过期key清除时间间隔， 0表示不清理过期数据，可用做兜底
         shards: 256 #分区数
         max_size: 512 #缓存占用最大内部，单位M
         serialization_type: 0 #序列化方式，对应codec.SerializationType, 0pb,1jce,2json,3flat buffer,4空序列化方式
         max_entries_in_window: 2048 #最大实体数量
         max_entries_size: 4096 #单个实体允许最大字节数
         allow_use_expired_entry: true # 是否允许在请求失败的情况下，使用过期数据兜底
```

```
// serialization_type 值定义如下:
    // SerializationTypePB is protobuf serialization code.
    SerializationTypePB = 0
    // SerializationTypeJCE is jce serialization code.
    SerializationTypeJCE = 1
    // SerializationTypeJSON is json serialization code.
    SerializationTypeJSON = 2
    // SerializationTypeFlatBuffer is flatbuffer serialization code.
    SerializationTypeFlatBuffer = 3
    // SerializationTypeNoop is bytes empty serialization code.
    SerializationTypeNoop = 4
    // SerializationTypeXML is xml serialization code (application/xml for http).
    SerializationTypeXML = 5
    // SerializationTypeTextXML is xml serialization code (text/xml for http).
    SerializationTypeTextXML = 6
```