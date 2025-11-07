# Dockerfile for Development Container
# Base image with common tools
FROM debian:bookworm-slim

# Install common dependencies needed by all projects (Go, Node, Java tools will be installed later or via features)
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    git \
    curl \
    unzip \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Set default user
ARG USERNAME=vscode
ARG USER_UID=1000
ARG USER_GID=$USER_UID
RUN groupadd --gid $USER_GID $USERNAME \
    && useradd -s /bin/bash --uid $USER_UID --gid $USER_GID -m $USERNAME \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

USER $USERNAME