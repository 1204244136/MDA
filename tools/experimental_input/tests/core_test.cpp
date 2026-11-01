#include "../ExperimentalInputCore.h"
#include "../Probe.h"

#include <cassert>
#include <string>
#include <utility>
#include <vector>

struct FakeCursor : experimental_input::Cursor {
    bool active = true;
    experimental_input::Point pos { 0, 0 };
    bool focused() const override { return active; }
    bool target(int x, int y, experimental_input::Point& p) const override { p = {x + 100, y + 200}; return x >= 0 && y >= 0; }
    bool position(experimental_input::Point& p) const override { p = pos; return true; }
    void pause() const override { }
};

struct FakeBackend : experimental_input::Backend {
    FakeCursor& cursor;
    bool fail_press = false, fail_release = false, fail_wheel = false;
    std::vector<std::string> events;
    explicit FakeBackend(FakeCursor& c) : cursor(c) { }
    bool move(int32_t x, int32_t y) override { assert(std::abs(x) <= 48 && std::abs(y) <= 48); cursor.pos.x += x; cursor.pos.y += y; return true; }
    bool button(int32_t b, bool down) override { events.push_back(std::to_string(b) + (down ? "+" : "-")); return !fail_press || !down; }
    bool key(int32_t k, bool down) override { events.push_back(std::to_string(k) + (down ? "+" : "-")); return !fail_press || !down; }
    bool wheel(int32_t d) override { events.push_back("wheel" + std::to_string(d)); return !fail_wheel; }
    bool release_all() override { events.push_back("release"); return !fail_release; }
};

struct FakeProbe : experimental_input::ProbeIO {
    experimental_input::Point pos {50, 50};
    std::vector<std::pair<int,int>> moves;
    bool no_motion = false, wrong_motion = false, interrupt_wait = false, bad_return = false;
    int pauses = 0;
    bool position(experimental_input::Point& p) override { p = pos; return true; }
    bool move(int32_t x, int32_t y) override {
        moves.emplace_back(x,y);
        if (!no_motion) { pos.x += x; pos.y += y; }
        if (wrong_motion && moves.size() == 1) pos.y += 20;
        if (bad_return && moves.size() == 2) pos.x += 10;
        return true;
    }
    void pause(int) override { if (++pauses == 3 && interrupt_wait) pos.y += 3; }
};

int main()
{
    FakeCursor cursor;
    FakeBackend backend(cursor);
    experimental_input::Session session(backend, cursor);
    assert(session.touch_down(0, 0, 20, 30));
    assert(cursor.pos.x >= 119 && cursor.pos.x <= 121 && cursor.pos.y >= 229 && cursor.pos.y <= 231);
    assert(session.touch_move(0, 30, 40));
    assert(session.touch_up(0));
    const auto before_hover = backend.events.size();
    assert(session.touch_move(0, 45, 55));
    assert(cursor.pos.x >= 144 && cursor.pos.x <= 146 && cursor.pos.y >= 254 && cursor.pos.y <= 256);
    assert(backend.events.size() == before_hover);
    assert(session.scroll(0, 120));
    assert(backend.events.back() == "wheel120");
    assert(session.key('A', true));
    assert(session.key('A', false));
    assert(session.relative_move(-100, 120));
    assert(session.key('B', true));
    cursor.active = false;
    assert(session.release_if_unfocused());
    assert(backend.events.back() == "release");
    assert(!session.key('C', true));
    cursor.active = true;
    backend.fail_press = true;
    backend.fail_release = true;
    assert(!session.key('D', true));
    auto before = backend.events.size();
    assert(!session.key('E', true));
    assert(backend.events.size() == before + 1 && backend.events.back() == "release");
    backend.fail_release = false;
    assert(session.release());

    FakeCursor scroll_cursor;
    FakeBackend scroll_backend(scroll_cursor);
    experimental_input::Session scroll_session(scroll_backend, scroll_cursor);
    assert(!scroll_session.touch_move(0, -1, 10));
    assert(!scroll_session.scroll(0, 120));
    assert(scroll_backend.events.empty());
    assert(scroll_session.touch_move(0, 10, 10));
    assert(scroll_session.scroll(0, 60));
    scroll_cursor.active = false;
    assert(scroll_session.release_if_unfocused());
    scroll_cursor.active = true;
    assert(scroll_session.scroll(0, 60));
    assert(scroll_backend.events.empty());
    scroll_backend.fail_wheel = true;
    assert(!scroll_session.scroll(0, 60));
    scroll_backend.fail_wheel = false;
    auto after_failed_wheel = scroll_backend.events.size();
    assert(scroll_session.scroll(0, 60));
    assert(scroll_backend.events.size() == after_failed_wheel);

    FakeProbe ok;
    experimental_input::ProbeTrace trace;
    assert(experimental_input::probe(ok, 5, 0, &trace) == experimental_input::ProbeResult::success);
    assert(ok.moves.size() == 2 && trace.have_origin && trace.have_outward && trace.have_last);
    assert(trace.dx == 5 && trace.dy == 0 && trace.outward.x - trace.origin.x == 5 && trace.last.x == trace.origin.x);
    FakeProbe none; none.no_motion = true; assert(experimental_input::probe(none, 5, 0) == experimental_input::ProbeResult::no_motion); assert(none.moves.size() == 1);
    FakeProbe wrong; wrong.wrong_motion = true;
    assert(experimental_input::probe(wrong, 5, 0, &trace) == experimental_input::ProbeResult::unexpected_motion);
    assert(wrong.moves.size() == 1 && trace.have_outward && trace.last.y - trace.origin.y == 20);
    FakeProbe interrupted; interrupted.interrupt_wait = true; assert(experimental_input::probe(interrupted, 5, 0) == experimental_input::ProbeResult::unexpected_motion); assert(interrupted.moves.size() == 1);
    FakeProbe bad; bad.bad_return = true; assert(experimental_input::probe(bad, 0, -7) == experimental_input::ProbeResult::return_failed); assert(bad.moves.size() == 2);
    FakeProbe invalid; assert(experimental_input::probe(invalid, 40, 0) == experimental_input::ProbeResult::invalid_delta); assert(invalid.moves.empty());
}
