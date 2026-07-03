#pragma once

/**
 * @brief Structured, recursive ostream formatting for custom and standard types.
 *
 * `logfmt` renders values to a stream while tracking indentation for you, so
 * nested structures (vectors of maps of custom types, …) come out consistently
 * formatted. Every value is printed through a `to_ostream(context&, const T&)`
 * function; the `context` carries the target stream, the current indentation
 * level, a recursion-depth guard, and a one-line/multi-line mode flag.
 *
 * ### Entry points
 * - `logfmt::to_string(value)`          -> pretty, multi-line string.
 * - `logfmt::to_string_one_line(value)` -> compact, single-line string.
 * - `logfmt::to_ostream(ctx, value)`    -> write directly to a `context`.
 * - Opt-in `operator<<` overloads for the standard containers live at the
 *   bottom of this header; enable them with `using namespace logfmt;` (or
 *   `using logfmt::operator<<;`) in the scope that needs them.
 *
 * ### Extending for a custom type `T` (recommended: free function)
 * Provide a free `to_ostream` overload found via ADL. Drive layout through the
 * context helpers rather than emitting `'\n'`/spaces yourself, so the SAME
 * implementation renders correctly in both multi-line and one-line mode (in
 * one-line mode `indent()`/`newline()` are automatically suppressed):
 * @code
 * struct Node {
 *     std::string name;
 *     std::vector<Node> children;
 * };
 *
 * void to_ostream(logfmt::context &ctx, const Node &node) {
 *     ctx << "node { name: " << node.name;
 *     if (!node.children.empty()) {
 *         ctx.newline();
 *         ctx.inc_indent();
 *         for (size_t i = 0; i < node.children.size(); ++i) {
 *             ctx.indent();
 *             to_ostream(ctx, node.children[i]);   // recurses, keeps context
 *             if (i + 1 < node.children.size()) ctx << ",";
 *             ctx.newline();
 *         }
 *         ctx.dec_indent();
 *         ctx.indent();
 *     }
 *     ctx << " }";
 * }
 * @endcode
 *
 * As an alternative to the free function, a type may expose a member
 * `void to_ostream(logfmt::context &ctx) const;` — the generic fallback below
 * forwards to it. A free overload always takes precedence when both exist.
 *
 * ### Context helpers
 * - `ctx << value`     : stream insertion for anything the stream supports.
 * - `ctx.indent()`     : write the current indentation (4 spaces per level).
 * - `ctx.newline()`    : write a newline.
 * - `ctx.inc_indent()` / `ctx.dec_indent()` : adjust the indentation level.
 * - In one-line mode `indent()` and `newline()` do nothing, so the same code
 *   produces compact output.
 *
 * ### Depth guard
 * Containers stop descending at `ctx.max_depth` (default 32) and print a
 * compact `[...]` / `{...}` placeholder, protecting against pathological or
 * self-referential structures.
 */

#include <array>
#include <cstddef>
#include <cstdint>
#include <iostream>
#include <iterator>
#include <map>
#include <optional>
#include <set>
#include <sstream>
#include <string>
#include <tuple>
#include <type_traits>
#include <unordered_set>
#include <utility>
#include <variant>
#include <vector>

namespace logfmt
{

struct context
{
    explicit context(std::ostream &os_) : os(os_) {}

    std::ostream &os;
    size_t indent_level = 0;
    size_t max_depth    = 32;    // Safety limit for recursive container nesting.
    bool   one_line     = false; // When set, indent()/newline() are suppressed.

    template <typename T> context &operator<<(const T &v)
    {
        os << v;
        return *this;
    }

    //  Overload for stream manipulators (std::endl, std::flush, …). Optional,
    //  but needed for full stream behaviour through the context.
    typedef std::ostream &(*StreamManipulator)(std::ostream &);
    context &operator<<(StreamManipulator manip)
    {
        os << manip;
        return *this;
    }

    // Write the current indentation. No-op in one-line mode. Uses a static
    // blank buffer for the common (shallow) case and falls back to a loop for
    // unusually deep nesting.
    void indent()
    {
        if (one_line)
            return;

        static const std::string spaces(512, ' ');
        size_t len = indent_level * 4;

        if (len <= spaces.size())
        {
            os.write(spaces.c_str(), len);
        }
        else
        {
            for (size_t i = 0; i < len; ++i)
                os.put(' ');
        }
    }

    void newline()
    {
        if (!one_line)
            os.put('\n');
    }

    void inc_indent() { indent_level++; }
    void dec_indent()
    {
        if (indent_level > 0)
        {
            indent_level--;
        }
    }
};

// --- forward declarations ---

template <typename T>
typename std::enable_if<std::is_arithmetic<T>::value>::type to_ostream(context &ctx, T v);
template <typename T>
typename std::enable_if<!std::is_arithmetic<T>::value>::type to_ostream(context &ctx, const T &v);
inline void to_ostream(context &ctx, const std::string &v);
inline void to_ostream(context &ctx, const char *v);
inline void to_ostream(context &ctx, uint8_t v);
inline void to_ostream(context &ctx, int8_t v);
template <typename K, typename V> void to_ostream(context &ctx, const std::pair<K, V> &kv);
template <typename T> void to_ostream(context &ctx, const std::optional<T> &v);
inline void to_ostream(context &ctx, std::monostate);
template <typename T, size_t N> void to_ostream(context &ctx, const std::array<T, N> &v);
template <typename T> void to_ostream(context &ctx, const std::vector<T> &v);
template <typename T> void to_ostream(context &ctx, const std::set<T> &v);
template <typename T> void to_ostream(context &ctx, const std::unordered_set<T> &v);
template <typename K, typename V> void to_ostream(context &ctx, const std::map<K, V> &v);
template <typename... Ts> void to_ostream(context &ctx, const std::tuple<Ts...> &t);

// --- container helper ---

// Renders [begin, end) as `prefix elem, elem, … suffix`. In multi-line mode
// each element sits on its own indented line; in one-line mode indent()/
// newline() collapse to nothing, yielding `prefix elem,elem suffix`.
template <typename It> void to_ostream_range(context &ctx, It begin_, It end_, char prefix, char suffix)
{
    if (begin_ == end_)
    {
        ctx << prefix << suffix;
        return;
    }

    // Safety: stop descending past the configured depth.
    if (ctx.indent_level >= ctx.max_depth)
    {
        ctx << prefix << "..." << suffix;
        return;
    }

    ctx << prefix;
    ctx.newline();
    ctx.inc_indent();
    for (It it = begin_; it != end_; ++it)
    {
        ctx.indent();
        to_ostream(ctx, *it);
        if (std::next(it) != end_)
        {
            ctx << ",";
        }
        ctx.newline();
    }
    ctx.dec_indent();
    ctx.indent();
    ctx << suffix;
}

// --- scalar / leaf implementations ---

// All built-in arithmetic types (int, long, size_t, float, double, …) stream
// directly. uint8_t/int8_t are handled separately below so they print as
// numbers rather than raw characters.
template <typename T>
typename std::enable_if<std::is_arithmetic<T>::value>::type to_ostream(context &ctx, T v)
{
    ctx << v;
}

inline void to_ostream(context &ctx, uint8_t v) { ctx << static_cast<unsigned>(v); }
inline void to_ostream(context &ctx, int8_t v) { ctx << static_cast<int>(v); }
inline void to_ostream(context &ctx, const std::string &v) { ctx << v; }
inline void to_ostream(context &ctx, const char *v) { ctx << v; }

// Generic fallback for non-arithmetic types without a dedicated overload:
// forward to a member `to_ostream(context&)`. A free `to_ostream` overload for
// the type, if provided, is a better match and wins over this template.
template <typename T>
typename std::enable_if<!std::is_arithmetic<T>::value>::type to_ostream(context &ctx, const T &v)
{
    v.to_ostream(ctx);
}

template <typename K, typename V> void to_ostream(context &ctx, const std::pair<K, V> &kv)
{
    ctx << "{ ";
    to_ostream(ctx, kv.first);
    ctx << ": ";
    to_ostream(ctx, kv.second);
    ctx << " }";
}

// std::optional prints its value when engaged, or `null` when empty. Common in
// generated DTOs for fields with explicit presence.
template <typename T> void to_ostream(context &ctx, const std::optional<T> &v)
{
    if (v)
        to_ostream(ctx, *v);
    else
        ctx << "null";
}

// std::monostate is the "empty" alternative of a std::variant (e.g. an unset
// oneof).
inline void to_ostream(context &ctx, std::monostate) { ctx << "(unset)"; }

template <typename T, size_t N> void to_ostream(context &ctx, const std::array<T, N> &v)
{
    to_ostream_range(ctx, v.begin(), v.end(), '[', ']');
}

template <typename T> void to_ostream(context &ctx, const std::vector<T> &v)
{
    to_ostream_range(ctx, v.begin(), v.end(), '[', ']');
}

template <typename T> void to_ostream(context &ctx, const std::set<T> &v)
{
    to_ostream_range(ctx, v.begin(), v.end(), '{', '}');
}

template <typename T> void to_ostream(context &ctx, const std::unordered_set<T> &v)
{
    to_ostream_range(ctx, v.begin(), v.end(), '{', '}');
}

template <typename K, typename V> void to_ostream(context &ctx, const std::map<K, V> &v)
{
    to_ostream_range(ctx, v.begin(), v.end(), '{', '}');
}

namespace detail
{
template <std::size_t I, typename... Ts>
typename std::enable_if<I == sizeof...(Ts), void>::type tuple_print(context &, const std::tuple<Ts...> &) { }

template <std::size_t I, typename... Ts>
typename std::enable_if<(I < sizeof...(Ts)), void>::type tuple_print(context &ctx, const std::tuple<Ts...> &t)
{
    if (I != 0)
        ctx << ", ";
    to_ostream(ctx, std::get<I>(t));
    tuple_print<I + 1>(ctx, t);
}
} // namespace detail

template <typename... Ts> void to_ostream(context &ctx, const std::tuple<Ts...> &t)
{
    ctx << "(";
    detail::tuple_print<0>(ctx, t);
    ctx << ")";
}

// Render any value in one-line mode. Delegates to the normal to_ostream
// dispatch with the context's one_line flag set, so it works for every type
// (containers, tuples, custom types) without a parallel overload set.
template <typename T> void to_ostream_one_line(context &ctx, const T &v)
{
    bool saved    = ctx.one_line;
    ctx.one_line  = true;
    to_ostream(ctx, v);
    ctx.one_line  = saved;
}

// --- string converters ---

template <typename T> std::string to_string(const T &v)
{
    std::stringstream ss;
    context ctx(ss);
    to_ostream(ctx, v);
    return ss.str();
}

template <typename T> std::string to_string_one_line(const T &v)
{
    std::stringstream ss;
    context ctx(ss);
    ctx.one_line = true;
    to_ostream(ctx, v);
    return ss.str();
}

} // namespace logfmt

// --- Opt-in operator<< overloads for standard containers ---
// These live in namespace logfmt, so `std::cout << vec` only finds them when
// the surrounding scope has `using namespace logfmt;` (or `using
// logfmt::operator<<;`). Keeping them namespaced avoids clobbering ADL for
// other libraries that format standard types.

namespace logfmt
{

template <typename A, typename B> std::ostream &operator<<(std::ostream &os, const std::pair<A, B> &v)
{
    logfmt::context ctx(os);
    to_ostream(ctx, v);
    return os;
}
template <typename T> std::ostream &operator<<(std::ostream &os, const std::set<T> &v)
{
    logfmt::context ctx(os);
    to_ostream(ctx, v);
    return os;
}
template <typename T> std::ostream &operator<<(std::ostream &os, const std::unordered_set<T> &v)
{
    logfmt::context ctx(os);
    to_ostream(ctx, v);
    return os;
}
template <typename T, size_t N> std::ostream &operator<<(std::ostream &os, const std::array<T, N> &v)
{
    logfmt::context ctx(os);
    to_ostream(ctx, v);
    return os;
}
template <typename T> std::ostream &operator<<(std::ostream &os, const std::vector<T> &v)
{
    logfmt::context ctx(os);
    to_ostream(ctx, v);
    return os;
}
template <typename K, typename V> std::ostream &operator<<(std::ostream &os, const std::map<K, V> &v)
{
    logfmt::context ctx(os);
    to_ostream(ctx, v);
    return os;
}
template <typename... Ts> std::ostream &operator<<(std::ostream &os, const std::tuple<Ts...> &t)
{
    logfmt::context ctx(os);
    to_ostream(ctx, t);
    return os;
}
} // namespace logfmt
