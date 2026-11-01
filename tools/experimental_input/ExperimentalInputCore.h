#pragma once

#include <algorithm>
#include <array>
#include <chrono>
#include <cmath>
#include <cstdint>

namespace experimental_input {

struct Point { int x; int y; };

struct Backend {
    virtual ~Backend() = default;
    virtual bool move(int32_t dx, int32_t dy) = 0;
    virtual bool button(int32_t button, bool down) = 0;
    virtual bool key(int32_t vk, bool down) = 0;
    virtual bool wheel(int32_t delta) = 0;
    virtual bool release_all() = 0;
};

struct Cursor {
    virtual ~Cursor() = default;
    virtual bool focused() const = 0;
    virtual bool target(int x, int y, Point& screen) const = 0;
    virtual bool position(Point& screen) const = 0;
    virtual void pause() const = 0;
};

// The caller serializes all operations, including release after focus loss.
class Session {
public:
    Session(Backend& backend, Cursor& cursor) : backend_(backend), cursor_(cursor) { contacts_.fill(-1); }

    bool release()
    {
        wheel_remainder_ = 0;
        if (!dirty_) return true;
        if (!backend_.release_all()) { uncertain_ = true; return false; }
        dirty_ = false;
        uncertain_ = false;
        keys_.fill(false);
        contacts_.fill(-1);
        return true;
    }

    bool release_if_unfocused() { return (!uncertain_ && cursor_.focused()) || release(); }

    bool touch_down(int contact, int button, int x, int y)
    {
        if (!recover()) return false;
        if (contact < 0 || contact >= 3 || button < 0 || button >= 3 || contacts_[contact] != -1 ||
            std::find(contacts_.begin(), contacts_.end(), button) != contacts_.end()) return fail();
        if (!move_to(x, y)) return fail();
        // A failed call may still have delivered an input event.
        dirty_ = true;
        if (!backend_.button(button, true)) return fail();
        contacts_[contact] = button;
        return true;
    }

    bool touch_move(int contact, int x, int y)
    {
        if (!recover()) { scroll_move_failed_ = true; return false; }
        if (contact < 0 || contact >= 3 || !move_to(x, y)) {
            scroll_move_failed_ = true;
            return fail();
        }
        scroll_move_failed_ = false;
        return true;
    }

    bool touch_up(int contact)
    {
        if (!recover()) return false;
        if (contact < 0 || contact >= 3 || contacts_[contact] == -1) return fail();
        // Releases are allowed even after the target loses focus.
        if (!backend_.button(contacts_[contact], false)) return fail();
        contacts_[contact] = -1;
        return true;
    }

    bool key(int vk, bool down)
    {
        if (!recover()) return false;
        if (vk < 1 || vk > 254) return fail();
        if (down && !cursor_.focused()) return fail();
        if (keys_[vk] == down) return true;
        if (down) dirty_ = true;
        if (!backend_.key(vk, down)) return fail();
        keys_[vk] = down;
        return true;
    }

    bool relative_move(int dx, int dy)
    {
        if (!recover()) return false;
        if (!cursor_.focused()) return fail();
        int64_t x = dx, y = dy;
        const auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(2);
        while (x || y) {
            if (!cursor_.focused() || std::chrono::steady_clock::now() >= deadline) return fail();
            const auto steps = std::max<int64_t>(1, (std::max(std::abs(x), std::abs(y)) + 47) / 48);
            const auto sx = static_cast<int32_t>(x / steps), sy = static_cast<int32_t>(y / steps);
            if (!backend_.move(sx, sy)) return fail();
            x -= sx;
            y -= sy;
            cursor_.pause();
        }
        return true;
    }

    bool scroll(int dx, int dy)
    {
        if (scroll_move_failed_) { scroll_move_failed_ = false; return fail(); }
        if (!recover()) return false;
        if (!cursor_.focused() || dx || std::abs(static_cast<int64_t>(dy)) > 12000) return fail();
        wheel_remainder_ += dy;
        while (std::abs(wheel_remainder_) >= 120) {
            const int delta = wheel_remainder_ > 0 ? 120 : -120;
            if (!cursor_.focused() || !backend_.wheel(delta)) return fail();
            wheel_remainder_ -= delta;
        }
        return true;
    }

private:
    bool recover() { return !uncertain_ || release(); }
    bool fail() { release(); return false; }

    bool move_to(int x, int y)
    {
        Point target {};
        if (!cursor_.focused() || !cursor_.target(x, y, target)) return false;
        const auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(2);
        while (std::chrono::steady_clock::now() < deadline) {
            Point current {};
            if (!cursor_.focused() || !cursor_.position(current)) return false;
            const int64_t dx = static_cast<int64_t>(target.x) - current.x;
            const int64_t dy = static_cast<int64_t>(target.y) - current.y;
            if (std::abs(dx) <= 1 && std::abs(dy) <= 1) return true;
            const double scale = std::min(0.5, 48.0 / static_cast<double>(std::max(std::abs(dx), std::abs(dy))));
            const auto sx = static_cast<int32_t>(std::lround(dx * scale));
            const auto sy = static_cast<int32_t>(std::lround(dy * scale));
            if (!backend_.move(sx, sy)) return false;
            cursor_.pause();
        }
        return false;
    }

    Backend& backend_;
    Cursor& cursor_;
    std::array<bool, 255> keys_ {};
    std::array<int, 3> contacts_ {};
    int64_t wheel_remainder_ {};
    bool dirty_ {};
    bool uncertain_ {};
    bool scroll_move_failed_ {};
};

} // namespace experimental_input
