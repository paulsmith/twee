cmake_minimum_required(VERSION 3.19)
project(twee LANGUAGES C)

# Pull ghostty source and build libghostty-vt via its CMake integration.
# Pinned to the same commit go-libghostty (29fdb3130d) ships against, so
# the C ABI matches the Go bindings.
include(FetchContent)
FetchContent_Declare(ghostty
    GIT_REPOSITORY https://github.com/ghostty-org/ghostty.git
    GIT_TAG 2ed382a15566b267c32fae440b065f7844b15bfb
)
FetchContent_MakeAvailable(ghostty)
