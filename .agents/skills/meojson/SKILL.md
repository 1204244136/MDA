---
name: meojson
description: meojson C++ JSON 库使用指南。用于 MaaFramework C++ 项目中的 JSON 解析、序列化、MEO_JSONIZATION、json::value 操作、ext::jsonization 自定义类型支持，以及 Custom 参数解析。
---

# meojson 使用指南

meojson 是 MaaFramework 依赖中常用的 header-only C++ JSON 库。通常通过以下头文件引入：

```cpp
#include <meojson/json.hpp>
```

## 核心类型

| 类型           | 说明                                                         |
| -------------- | ------------------------------------------------------------ |
| `json::value`  | 通用 JSON 值：null / bool / number / string / array / object |
| `json::array`  | JSON 数组，包装 `std::vector<json::value>`                   |
| `json::object` | JSON 对象，包装 `std::map<std::string, json::value>`         |

## 解析

```cpp
auto opt = json::parse(str);      // std::optional<json::value>
auto file = json::open(path);     // 从文件读取
auto jsonc = json::parsec(str);   // JSONC，支持注释
```

推荐安全模式：

```cpp
template <typename T>
T ParseParam(const char* text)
{
    if (text == nullptr || std::strlen(text) == 0) {
        return T {};
    }

    auto opt = json::parse(text);
    if (!opt) {
        LogError << "failed to parse json param" << VAR(text);
        return T {};
    }

    T result {};
    if (!result.from_json(*opt)) {
        LogError << "failed to deserialize json param" << VAR(text);
        return T {};
    }
    return result;
}
```

避免：

```cpp
json::parse(str).value_or(json::object {}).as<T>();
```

这会静默吞掉 parse 失败，并可能在 required 字段缺失时抛异常。

## 构造 JSON 值

```cpp
json::value v1 = 42;
json::value v2 = "hello";
json::value v3 = true;
json::value v4 = nullptr;

json::array arr { 1, 2, "three" };
json::object obj {
    { "key1", "value1" },
    { "key2", 42 },
};

std::vector<int> vec = { 1, 2, 3 };
json::value v5 = vec;

std::map<std::string, int> map = { { "a", 1 } };
json::value v6 = map;
```

## 读取值

### 类型检查

```cpp
v.is_null();
v.is_boolean();
v.is_number();
v.is_string();
v.is_array();
v.is_object();
v.is<int>();
```

### 直接访问

```cpp
v.as_string();
v.as_string_view();
v.as_integer();
v.as_double();
v.as_boolean();
v.as_array();
v.as_object();
v.as<T>();
```

`as_*()` / `as<T>()` 类型不匹配时可能抛异常，只在确定数据合法时使用。

### 安全访问

```cpp
auto name = v.find<std::string>("name");
if (name) {
    // use *name
}

std::string label = v.get("label", "default");
int count = v.get("config", "count", 0);

if (v.exists("key")) {
    // ...
}
```

## 序列化

```cpp
v.dumps();       // 紧凑字符串
v.dumps(4);      // 4 空格缩进
v.format();      // pretty print
```

写回 MaaFramework detail 的常见模式：

```cpp
template <typename T>
void WriteJsonDetail(MaaStringBuffer* out_detail, const T& payload)
{
    if (out_detail == nullptr) {
        return;
    }
    const std::string json_text = json::value(payload).dumps();
    MaaStringBufferSet(out_detail, json_text.c_str());
}
```

## Object 合并

```cpp
json::value merged = obj1 | obj2;  // 右侧覆盖同名键
obj1 |= obj2;
```

## MEO_JSONIZATION

`MEO_JSONIZATION(fields...)` 生成 `to_json()`、`check_json()`、`from_json()`。

```cpp
struct LocateOutput {
    int status = 0;
    std::string message;
    std::string map_name;
    int x = 0;
    int y = 0;

    MEO_JSONIZATION(status, message, MEO_OPT map_name, MEO_OPT x, MEO_OPT y)
};
```

序列化：

```cpp
json::value j = data;
```

反序列化推荐：

```cpp
LocateOutput data {};
if (!data.from_json(j)) {
    LogError << "failed to deserialize" << VAR(j);
}
```

## MEO_OPT

默认字段都是 required。用 `MEO_OPT` 标记可选字段，缺失时保留默认值：

```cpp
struct Options {
    double threshold = 0.7;
    bool enabled = true;

    MEO_JSONIZATION(MEO_OPT threshold, MEO_OPT enabled)
};
```

每个可选字段都需要自己的 `MEO_OPT`。

## MEO_KEY

当 JSON key 与 C++ 字段名不同，使用 `MEO_KEY`：

```cpp
struct TemplateParam {
    std::vector<std::string> template_;

    MEO_JSONIZATION(MEO_KEY("template") template_)
};
```

与 `MEO_OPT` 组合时顺序通常为：

```cpp
MEO_OPT MEO_KEY("default") default_
```

## 子宏

| 宏                     | 生成内容       |
| ---------------------- | -------------- |
| `MEO_TOJSON(...)`      | `to_json()`    |
| `MEO_FROMJSON(...)`    | `from_json()`  |
| `MEO_CHECKJSON(...)`   | `check_json()` |
| `MEO_JSONIZATION(...)` | 全部三个       |

## ext::jsonization

对不拥有源码的类型，特化 `json::ext::jsonization<T>`：

```cpp
namespace json::ext {
template <>
class jsonization<MyRect> {
public:
    json::value to_json(const MyRect& rect) const
    {
        return json::array { rect.x, rect.y, rect.width, rect.height };
    }

    bool check_json(const json::value& value) const
    {
        return value.is<std::vector<int>>() && value.as_array().size() == 4;
    }

    bool from_json(const json::value& value, MyRect& rect) const
    {
        auto arr = value.as<std::vector<int>>();
        rect = MyRect { arr[0], arr[1], arr[2], arr[3] };
        return true;
    }
};
}
```

## Enum 反射

```cpp
enum class MyEnum {
    A,
    B,
    C,
    MEOJSON_ENUM_RANGE(A, C)
};

json::value j = MyEnum::B;
MyEnum e = j.as<MyEnum>();
```

## 常见坑

1. `json::parse` 返回 `std::optional`，必须检查。
2. `as_*()` / `as<T>()` 类型不匹配时可能抛异常。
3. 禁止 `.value_or(...).as<T>()` 静默吞解析错误。
4. `char` / `wchar_t` 构造被禁用，使用 `std::string` 或整数。
5. `ext::jsonization` 位于 `json::ext` 命名空间。
6. 每个可选字段都需要自己的 `MEO_OPT`。
7. `MEO_KEY` 与 `MEO_OPT` 组合时注意顺序。

## 参考

详细 API 见 [reference.md](reference.md)。
