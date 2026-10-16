# meojson API Reference

## `json::value`

### Constructors

```cpp
value();
value(bool b);
value(int num);
value(unsigned num);
value(long num);
value(long long num);
value(float num);
value(double num);
value(const char* str);
value(std::string str);
value(std::string_view str);
value(std::nullptr_t);
value(const array& arr);
value(const object& obj);
value(std::initializer_list<object::value_type>);

value(const std::optional<T>& value);
value(const std::shared_ptr<T>& value);
value(const std::unique_ptr<T>& value);

value(const std::vector<T>& value);
value(const std::set<T>& value);
value(const std::array<T, N>& value);
value(const std::pair<A, B>& value);
value(const std::tuple<Ts...>& value);
value(const std::variant<Ts...>& value);

value(const std::map<std::string, T>& value);
value(const std::unordered_map<std::string, T>& value);
```

Deleted:

```cpp
value(char) = delete;
value(wchar_t) = delete;
```

### Type Query

```cpp
bool valid() const noexcept;
bool empty() const noexcept;
bool is_null() const noexcept;
bool is_boolean() const noexcept;
bool is_number() const noexcept;
bool is_string() const noexcept;
bool is_array() const noexcept;
bool is_object() const noexcept;

template <typename T>
bool is() const noexcept;

template <typename T>
bool all() const;

value_type type() const noexcept;
std::string type_name() const;
```

### Access

```cpp
bool as_boolean() const;
int as_integer() const;
double as_double() const;
std::string as_string() const;
std::string_view as_string_view() const;
const array& as_array() const;
const object& as_object() const;

template <typename T>
T as() const;
```

### Safe Access

```cpp
template <typename T>
std::optional<T> find(std::string_view key) const;

template <typename T>
T get(std::string_view key, T default_value) const;

template <typename T>
T get(std::string_view key1, std::string_view key2, T default_value) const;

bool exists(std::string_view key) const;
bool contains(std::string_view key) const;
```

### Object / Array Access

```cpp
const json::value& operator[](std::string_view key) const;
json::value& operator[](std::string_view key);

const json::value& operator[](size_t index) const;
json::value& operator[](size_t index);
```

### Serialization

```cpp
std::string dumps() const;
std::string dumps(int indent) const;
std::string format() const;
```

## Parsing

```cpp
std::optional<json::value> json::parse(std::string_view text);
std::optional<json::value> json::parsec(std::string_view text);
std::optional<json::value> json::open(const std::filesystem::path& path);
```

## `json::array`

Common operations follow `std::vector<json::value>` style usage:

```cpp
json::array arr { 1, "two", true };
arr.emplace_back(3);
arr.push_back("four");

for (const auto& item : arr) {
    // ...
}
```

## `json::object`

Common operations follow `std::map<std::string, json::value>` style usage:

```cpp
json::object obj {
    { "name", "value" },
    { "count", 3 },
};

obj["enabled"] = true;

for (const auto& [key, value] : obj) {
    // ...
}
```

## JSONization Macros

```cpp
MEO_TOJSON(...)
MEO_FROMJSON(...)
MEO_CHECKJSON(...)
MEO_JSONIZATION(...)
MEO_OPT field
MEO_KEY("json_key") field
```

Typical usage:

```cpp
struct Data {
    std::string name;
    int count = 0;
    bool enabled = true;

    MEO_JSONIZATION(name, MEO_OPT count, MEO_OPT enabled)
};
```

## Custom Type Support

```cpp
namespace json::ext {
template <>
class jsonization<T> {
public:
    json::value to_json(const T& value) const;
    bool check_json(const json::value& value) const;
    bool from_json(const json::value& value, T& out) const;
};
}
```

## Enum Reflection

```cpp
enum class MyEnum {
    A,
    B,
    C,
    MEOJSON_ENUM_RANGE(A, C)
};
```

## Notes

- Prefer `from_json()` return-value checks for untrusted input.
- Use `as<T>()` only when data validity is already guaranteed.
- Use `find()` / `get()` for optional user or external data.
