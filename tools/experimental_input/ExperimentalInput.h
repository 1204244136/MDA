#pragma once

#include "MaaUtils/SafeWindows.hpp"

#include <array>
#include <chrono>
#include <filesystem>
#include <mutex>
#include <thread>

#include "Base/UnitBase.h"
#include "InputUtils.h"
#include "MaaUtils/Logger.h"

#include "ExperimentalInputCore.h"
#include "ExperimentalInputLibrary.h"

MAA_CTRL_UNIT_NS_BEGIN

constexpr MaaWin32InputMethod MdaRazerInputMethod = 1ULL << 12;
constexpr MaaWin32InputMethod MdaLogitechInputMethod = 1ULL << 11;

class ExperimentalInputCursor final : public experimental_input::Cursor {
public:
    explicit ExperimentalInputCursor(HWND hwnd) : hwnd_(hwnd) { }
    void set_enabled_path(std::filesystem::path path) { enabled_path_ = std::move(path); }
    bool focused() const override
    {
        if (enabled_path_.empty()) return false;
        const DWORD attributes = GetFileAttributesW(enabled_path_.c_str());
        if (attributes == INVALID_FILE_ATTRIBUTES || (attributes & FILE_ATTRIBUTE_DIRECTORY)) return false;
        const HWND root = hwnd_ ? GetAncestor(hwnd_, GA_ROOT) : nullptr;
        return root && IsWindow(root) && !IsIconic(root) && GetForegroundWindow() == root;
    }
    bool target(int x, int y, experimental_input::Point& out) const override
    {
        DpiScope dpi;
        RECT rect {};
        POINT p { x, y };
        if (!GetClientRect(hwnd_, &rect) || x < 0 || y < 0 || x >= rect.right || y >= rect.bottom ||
            !ClientToScreen(hwnd_, &p)) return false;
        out = { p.x, p.y };
        return true;
    }
    bool position(experimental_input::Point& out) const override
    {
        DpiScope dpi;
        POINT p {};
        if (!GetCursorPos(&p)) return false;
        out = { p.x, p.y };
        return true;
    }
    void pause() const override { std::this_thread::sleep_for(std::chrono::milliseconds(4)); }

private:
    struct DpiScope {
        DPI_AWARENESS_CONTEXT previous = SetThreadDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
        ~DpiScope() { if (previous) SetThreadDpiAwarenessContext(previous); }
    };
    HWND hwnd_ {};
    std::filesystem::path enabled_path_;
};

class ExperimentalInput final : public RelativeMoveInput {
public:
    explicit ExperimentalInput(HWND hwnd, const wchar_t* connector) : connector_(connector), cursor_(hwnd), session_(library_, cursor_) { }
    ~ExperimentalInput() override
    {
        watcher_.request_stop();
        if (watcher_.joinable()) watcher_.join();
        std::lock_guard lock(mutex_);
        if (!session_.release()) LogError << "Input connector could not release all inputs before shutdown";
        library_.close();
    }

    bool initialize()
    {
        HMODULE module = nullptr;
        if (!GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,
                reinterpret_cast<LPCWSTR>(&module_anchor), &module)) {
            LogError << "Input connector cannot locate its framework module";
            return false;
        }
        std::array<wchar_t, 32768> path {};
        const DWORD size = GetModuleFileNameW(module, path.data(), static_cast<DWORD>(path.size()));
        if (!size || size >= path.size()) {
            LogError << "Input connector cannot locate the MDA directory";
            return false;
        }
        const auto root = std::filesystem::path(path.data()).parent_path().parent_path();
        const auto enabled = root / L"config" / L"experimental-input.enabled";
        const auto attributes = GetFileAttributesW(enabled.c_str());
        if (attributes == INVALID_FILE_ATTRIBUTES || (attributes & FILE_ATTRIBUTE_DIRECTORY)) {
            LogError << "Experimental input is disabled; enable it in settings and restart MDA";
            return false;
        }
        cursor_.set_enabled_path(enabled);
        const auto connector = root / connector_;
        if (!library_.open(connector)) {
            LogError << "Input connector initialization failed: " << library_.last_error();
            return false;
        }
        watcher_ = std::jthread([this](std::stop_token stop) {
            while (!stop.stop_requested()) {
                std::this_thread::sleep_for(std::chrono::milliseconds(20));
                std::lock_guard lock(mutex_);
                if (!session_.release_if_unfocused()) LogError << "Input connector could not release inputs; will retry";
            }
        });
        return true;
    }

    MaaControllerFeature get_features() const override
    {
        return MaaControllerFeature_UseMouseDownAndUpInsteadOfClick | MaaControllerFeature_UseKeyboardDownAndUpInsteadOfClick;
    }
    bool click(int, int) override { return false; }
    bool swipe(int, int, int, int, int) override { return false; }
    bool click_key(int) override { return false; }
    bool input_text(const std::string&) override { LogError << "Input connector does not implement Unicode text input"; return false; }
    bool touch_down(int contact, int x, int y, int) override
    {
        std::lock_guard lock(mutex_);
        return checked(session_.touch_down(contact, GetMappedContact(contact), x, y));
    }
    bool touch_move(int contact, int x, int y, int) override
    {
        std::lock_guard lock(mutex_);
        return checked(session_.touch_move(contact, x, y));
    }
    bool touch_up(int contact) override { std::lock_guard lock(mutex_); return checked(session_.touch_up(contact)); }
    bool relative_move(int dx, int dy) override { std::lock_guard lock(mutex_); return checked(session_.relative_move(dx, dy)); }
    bool key_down(int vk) override { std::lock_guard lock(mutex_); return checked(session_.key(vk, true)); }
    bool key_up(int vk) override { std::lock_guard lock(mutex_); return checked(session_.key(vk, false)); }
    bool scroll(int dx, int dy) override { std::lock_guard lock(mutex_); return checked(session_.scroll(dx, dy)); }
    void inactive() override
    {
        std::lock_guard lock(mutex_);
        if (!session_.release()) LogError << "Input connector could not release all inputs on deactivation";
    }

private:
    static void module_anchor() { }
    static bool checked(bool ok) { if (!ok) LogError << "Input operation failed; release was attempted"; return ok; }
    std::filesystem::path connector_;
    experimental_input::Library library_;
    ExperimentalInputCursor cursor_;
    experimental_input::Session session_;
    std::mutex mutex_;
    std::jthread watcher_;
};

MAA_CTRL_UNIT_NS_END
