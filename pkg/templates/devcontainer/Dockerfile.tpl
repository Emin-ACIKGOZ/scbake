# Use the standard Microsoft Dev Container base image for Debian 12 (Bookworm).
# This is a lightweight, pre-optimized image that works well with Features.
FROM mcr.microsoft.com/devcontainers/base:debian-12

# Language toolchains (Go, Node, Java) are installed via "features" 
# in devcontainer.json. This keeps this Dockerfile clean and fast to build.