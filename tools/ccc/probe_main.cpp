#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>

#include <algorithm>
#include <array>
#include <chrono>
#include <cwchar>
#include <filesystem>
#include <iostream>
#include <random>
#include <string>
#include <thread>

#include "CccLibrary.h"
#include "Probe.h"

class ProbeIO final : public ccc::ProbeIO {
public:
    ProbeIO(ccc::Library& library, DWORD parent_pid) : library_(library), parent_pid_(parent_pid) { }
    bool position(ccc::Point& out) override
    {
        POINT p {};
        if (!GetPhysicalCursorPos(&p)) return false;
        out = { p.x, p.y };
        return true;
    }
    bool move(int32_t x, int32_t y) override
    {
        DWORD foreground_pid = 0;
        GetWindowThreadProcessId(GetForegroundWindow(), &foreground_pid);
        return foreground_pid == parent_pid_ && library_.move(x, y);
    }
    void pause(int ms) override { std::this_thread::sleep_for(std::chrono::milliseconds(ms)); }
private:
    ccc::Library& library_;
    DWORD parent_pid_ {};
};

int wmain(int argc, wchar_t** argv)
{
    if (argc != 3 || (std::wstring(argv[1]) != L"BBB" && std::wstring(argv[1]) != L"CCC")) {
        std::cerr << "只能检测 BBB 或 CCC。"; return 1;
    }
    wchar_t* end = nullptr;
    const auto parent_pid = std::wcstoul(argv[2], &end, 10);
    if (!parent_pid || !end || *end) { std::cerr << "无效的 MDA 进程。"; return 1; }
    DWORD foreground_pid = 0;
    GetWindowThreadProcessId(GetForegroundWindow(), &foreground_pid);
    if (foreground_pid != parent_pid) { std::cerr << "MDA 不在前台，检测已取消。"; return 1; }
    std::array<wchar_t, 32768> path {};
    const DWORD size = GetModuleFileNameW(nullptr, path.data(), static_cast<DWORD>(path.size()));
    if (!size || size >= path.size()) { std::cerr << "无法定位 MDA 目录。"; return 1; }
    const auto root = std::filesystem::path(path.data()).parent_path().parent_path();
    const auto enabled = root / L"config" / L"experimental-input.enabled";
    const auto attributes = GetFileAttributesW(enabled.c_str());
    if (attributes == INVALID_FILE_ATTRIBUTES || (attributes & FILE_ATTRIBUTE_DIRECTORY)) {
        std::cerr << "实验性输入尚未开启。"; return 1;
    }
    const auto file = std::wstring(argv[1]) == L"BBB" ? L"bbb_connector.dll" : L"ccc_connector.dll";
    const auto connector_path = root / file;
    ccc::Library library;
    if (!library.open(connector_path)) {
        const auto code = library.last_system_error();
        if (code == ERROR_SHARING_VIOLATION || code == ERROR_LOCK_VIOLATION) {
            std::cerr << "输入连接器正在使用，请先断开当前连接。";
        } else if (code == ERROR_PATH_NOT_FOUND || code == ERROR_FILE_NOT_FOUND) {
            std::cerr << "配置目录不存在，无法检测。";
        } else if (code == ERROR_ACCESS_DENIED) {
            std::cerr << "无法访问配置目录，请检查权限。";
        } else if (GetFileAttributesW(connector_path.c_str()) == INVALID_FILE_ATTRIBUTES &&
                   (GetLastError() == ERROR_FILE_NOT_FOUND || GetLastError() == ERROR_PATH_NOT_FOUND)) {
            std::cerr << "未找到连接器 DLL，请将其放入 MDA 目录。";
        } else {
            std::cerr << "连接器无法启动，请检查 DLL 和设备。";
        }
        return 1;
    }
    ProbeIO io(library, parent_pid);
    ccc::Point origin {};
    if (!io.position(origin)) { std::cerr << "无法读取光标位置。"; return 1; }
    POINT p { origin.x, origin.y };
    MONITORINFO monitor {};
    monitor.cbSize = sizeof(MONITORINFO);
    if (!GetMonitorInfoW(MonitorFromPoint(p, MONITOR_DEFAULTTONULL), &monitor)) {
        std::cerr << "光标不在显示器范围内。"; return 1;
    }
    std::random_device rng;
    std::array<std::pair<int, int>, 4> directions {{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}};
    std::shuffle(directions.begin(), directions.end(), rng);
    const int distance = std::uniform_int_distribution<int>(4, 8)(rng);
    for (const auto [x, y] : directions) {
        const int tx = origin.x + x * distance, ty = origin.y + y * distance;
        if (tx < monitor.rcMonitor.left + 2 || tx >= monitor.rcMonitor.right - 2 ||
            ty < monitor.rcMonitor.top + 2 || ty >= monitor.rcMonitor.bottom - 2) continue;
        const auto result = ccc::probe(io, x * distance, y * distance);
        switch (result) {
        case ccc::ProbeResult::success:
            std::cout << "鼠标输入可用。";
            return 0;
        case ccc::ProbeResult::no_motion: std::cerr << "鼠标输入不可用，请检查连接器。"; break;
        case ccc::ProbeResult::preexisting_motion: std::cerr << "检测已取消，请松开鼠标后重试。"; break;
        case ccc::ProbeResult::unexpected_motion: std::cerr << "检测已中止，鼠标位置异常。"; break;
        case ccc::ProbeResult::return_failed: std::cerr << "鼠标输入异常，未能回到原位。"; break;
        case ccc::ProbeResult::move_failed: std::cerr << "检测未完成，请检查连接器并保持窗口在前台。"; break;
        default: std::cerr << "检测未完成，请重试。"; break;
        }
        return 1;
    }
    std::cerr << "光标离屏幕边缘太近，请移到屏幕中央再试。";
    return 1;
}
