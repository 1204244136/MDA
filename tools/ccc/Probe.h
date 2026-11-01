#pragma once

#include "CccCore.h"

#include <cstdint>
#include <cstdlib>

namespace ccc {

enum class ProbeResult { success, invalid_delta, position_error, preexisting_motion, move_failed, no_motion, unexpected_motion, return_failed };

struct ProbeTrace {
    Point origin {}, outward {}, last {};
    int dx {}, dy {};
    bool have_origin {}, have_outward {}, have_last {};
};

struct ProbeIO {
    virtual ~ProbeIO() = default;
    virtual bool position(Point&) = 0;
    virtual bool move(int32_t dx, int32_t dy) = 0;
    virtual void pause(int milliseconds) = 0;
};

inline bool close_enough(Point a, Point b, int tolerance)
{
    return std::abs(static_cast<int64_t>(a.x) - b.x) <= tolerance &&
           std::abs(static_cast<int64_t>(a.y) - b.y) <= tolerance;
}

// At most two movements. Never tries to correct an unexpected position.
inline ProbeResult probe(ProbeIO& io, int dx, int dy, ProbeTrace* trace = nullptr)
{
    if (trace) { *trace = {}; trace->dx = dx; trace->dy = dy; }
    const auto ax = std::abs(static_cast<int64_t>(dx));
    const auto ay = std::abs(static_cast<int64_t>(dy));
    if (!((dx == 0 && ay >= 4 && ay <= 8) ||
          (dy == 0 && ax >= 4 && ax <= 8))) return ProbeResult::invalid_delta;
    Point origin {}, current {};
    if (!io.position(origin)) return ProbeResult::position_error;
    if (trace) { trace->origin = origin; trace->have_origin = true; }
    io.pause(100);
    if (!io.position(current)) return ProbeResult::position_error;
    if (trace) { trace->last = current; trace->have_last = true; }
    if (!close_enough(current, origin, 0)) return ProbeResult::preexisting_motion;
    if (!io.move(dx, dy)) return ProbeResult::move_failed;
    io.pause(120);
    if (!io.position(current)) return ProbeResult::position_error;
    if (trace) { trace->outward = current; trace->last = current; trace->have_outward = true; trace->have_last = true; }
    if (close_enough(current, origin, 1)) return ProbeResult::no_motion;
    const auto expected_x = static_cast<int64_t>(origin.x) + dx;
    const auto expected_y = static_cast<int64_t>(origin.y) + dy;
    if (std::abs(static_cast<int64_t>(current.x) - expected_x) > 2 ||
        std::abs(static_cast<int64_t>(current.y) - expected_y) > 2) return ProbeResult::unexpected_motion;
    const Point outward = current;
    io.pause(150);
    if (!io.position(current)) return ProbeResult::position_error;
    if (trace) trace->last = current;
    if (!close_enough(current, outward, 0)) return ProbeResult::unexpected_motion;
    if (!io.move(-dx, -dy)) return ProbeResult::move_failed;
    io.pause(120);
    if (!io.position(current)) return ProbeResult::position_error;
    if (trace) trace->last = current;
    return close_enough(current, origin, 2) ? ProbeResult::success : ProbeResult::return_failed;
}

} // namespace ccc
