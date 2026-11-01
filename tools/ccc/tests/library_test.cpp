#include "../CccLibrary.h"

#include <cassert>
#include <filesystem>
#include <string>

int main(int argc, char** argv)
{
    assert(argc == 2);
    const auto dir = std::filesystem::absolute(argv[1]);
    {
        ccc::Library x;
        assert(!x.open(dir / "absent.dll"));
        assert(x.last_error().find("Cannot load") != std::string::npos);
    }
    if (std::filesystem::exists(dir / "x86.dll")) {
        ccc::Library x;
        assert(!x.open(dir / "x86.dll"));
        assert(x.last_error().find("Cannot load") != std::string::npos);
    }
    {
        ccc::Library x;
        assert(!x.open(dir / "bad-machine.dll"));
        assert(x.last_error().find("Cannot load") != std::string::npos);
    }
    {
        ccc::Library x;
        assert(!x.open(dir / "missing.dll"));
        assert(x.last_error().find("ccc_wheel") != std::string::npos);
    }
    {
        ccc::Library x;
        assert(!x.open(dir / "version.dll"));
        assert(x.last_error().find("ABI version") != std::string::npos);
    }
    {
        const auto path = dir / "open-fail.dll";
        HMODULE hold = LoadLibraryW(path.c_str());
        assert(hold);
        auto opens = reinterpret_cast<int (*)()>(GetProcAddress(hold, "mock_open_count"));
        auto closes = reinterpret_cast<int (*)()>(GetProcAddress(hold, "mock_close_count"));
        { ccc::Library x; assert(!x.open(path)); }
        assert(opens() == 1 && closes() == 1);
        FreeLibrary(hold);
    }
    {
        const auto lock_path = dir / "config/experimental-input.lock";
        HANDLE held = CreateFileW(lock_path.c_str(), GENERIC_READ | GENERIC_WRITE, 0, nullptr, OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
        assert(held != INVALID_HANDLE_VALUE);
        { ccc::Library x; assert(!x.open(dir / "good.dll")); assert(x.last_system_error() == ERROR_SHARING_VIOLATION); }
        CloseHandle(held);
    }
    {
        const auto missing_config = dir / "missing-config";
        std::filesystem::create_directory(missing_config);
        ccc::Library x;
        assert(!x.open(missing_config / "good.dll"));
        assert(x.last_system_error() == ERROR_PATH_NOT_FOUND);
    }
    {
        ccc::Library x;
        assert(x.open(dir / "good.dll"));
        assert(x.move(-48, 48));
        assert(x.button(0, true));
        assert(x.button(0, false));
        assert(x.key('A', true));
        assert(x.key('A', false));
        assert(x.wheel(120));
        assert(x.release_all());
        { ccc::Library other; assert(!other.open(dir / "good.dll")); }
        x.close();
        ccc::Library other;
        assert(other.open(dir / "good.dll"));
    }
}
