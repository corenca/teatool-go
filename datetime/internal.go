package datetime

import "sync"

// locations 缓存已加载的时区，避免重复读取 tzdata。
var locations sync.Map
