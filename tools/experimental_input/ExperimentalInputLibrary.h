#pragma once

#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>

#include <atomic>
#include <filesystem>
#include <string>

#include "ExperimentalInputCore.h"
#include "ExperimentalInputApi.h"

namespace experimental_input {

class Library final : public Backend {
public:
    ~Library() override { close(); }
    Library(const Library&) = delete;
    Library& operator=(const Library&) = delete;
    Library() = default;

    bool open(const std::filesystem::path& path)
    {
        last_system_error_ = ERROR_SUCCESS;
        if (owns_slot_ || dll_) return error("Input connector is already open or initializing");
        if (sizeof(void*) != 8) return error("Input connector requires a Windows x64 process");
        bool expected = false;
        if (!in_use_.compare_exchange_strong(expected, true)) return error("Input connector is already in use in this process");
        owns_slot_ = true;
        if (!path.is_absolute()) return error("Input connector path must be absolute");
        const auto lock_path = path.parent_path() / L"config" / L"experimental-input.lock";
        lock_ = CreateFileW(lock_path.c_str(), GENERIC_READ | GENERIC_WRITE, 0, nullptr, OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
        if (lock_ == INVALID_HANDLE_VALUE) {
            last_system_error_ = GetLastError();
            return error("Cannot access experimental input lock (Windows error " + std::to_string(last_system_error_) + ")");
        }
        dll_ = LoadLibraryExW(path.c_str(), nullptr, LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR | LOAD_LIBRARY_SEARCH_SYSTEM32);
        if (!dll_) return error("Cannot load the configured connector DLL (Windows error " + std::to_string(GetLastError()) + ")");
        if (!bind(version_, "ccc_api_version") || !bind(open_, "ccc_open") || !bind(close_, "ccc_close") ||
            !bind(move_, "ccc_mouse_move") || !bind(button_, "ccc_mouse_button") || !bind(key_, "ccc_key") ||
            !bind(wheel_, "ccc_wheel") || !bind(release_, "ccc_release_all")) return false;
        if (version_() != 1) return error("Unsupported connector ABI version; expected 1");
        open_attempted_ = true;
        if (open_() != 1) return error("Input connector initialization failed");
        ready_ = true;
        return true;
    }

    void close()
    {
        if (open_attempted_ && close_) close_();
        open_attempted_ = false;
        ready_ = false;
        if (dll_) FreeLibrary(dll_);
        dll_ = nullptr;
        if (lock_ != INVALID_HANDLE_VALUE) CloseHandle(lock_);
        lock_ = INVALID_HANDLE_VALUE;
        if (owns_slot_) in_use_.store(false);
        owns_slot_ = false;
    }

    const std::string& last_error() const { return error_; }
    DWORD last_system_error() const { return last_system_error_; }
    bool move(int32_t dx, int32_t dy) override { return ready_ && move_(dx, dy) == 1; }
    bool button(int32_t button, bool down) override { return ready_ && button_(button, down ? 1 : 0) == 1; }
    bool key(int32_t vk, bool down) override { return ready_ && key_(vk, down ? 1 : 0) == 1; }
    bool wheel(int32_t delta) override { return ready_ && wheel_(delta) == 1; }
    bool release_all() override { return ready_ && release_() == 1; }

private:
    bool error(std::string message) { error_ = std::move(message); return false; }
    template <typename T> bool bind(T& fn, const char* name)
    {
        fn = reinterpret_cast<T>(GetProcAddress(dll_, name));
        return fn != nullptr || error(std::string("Input connector missing export: ") + name);
    }

    HMODULE dll_ {};
    HANDLE lock_ { INVALID_HANDLE_VALUE };
    DWORD last_system_error_ { ERROR_SUCCESS };
    bool owns_slot_ {};
    bool open_attempted_ {};
    bool ready_ {};
    std::string error_;
    decltype(&ccc_api_version) version_ {};
    decltype(&ccc_open) open_ {};
    decltype(&ccc_close) close_ {};
    decltype(&ccc_mouse_move) move_ {};
    decltype(&ccc_mouse_button) button_ {};
    decltype(&ccc_key) key_ {};
    decltype(&ccc_wheel) wheel_ {};
    decltype(&ccc_release_all) release_ {};
    inline static std::atomic_bool in_use_ { false };
};

} // namespace experimental_input
