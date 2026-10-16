{ pkgs, ... }:

{
  cachix.enable = false;

  packages = [
    pkgs.go_1_25
    pkgs.python3
    pkgs.protobuf
    pkgs.protoc-gen-go
    pkgs.openssl
  ];

  enterShell = ''
    unset SDKROOT DEVELOPER_DIR NIX_CFLAGS_COMPILE NIX_LDFLAGS
    export DEVELOPER_DIR="$(/usr/bin/xcode-select -p)"
    export SDKROOT="$(/usr/bin/xcrun --sdk macosx --show-sdk-path)"
  '';

  tasks."dev:build" = {
    description = "Build the proxy and menu without launching or installing them";
    exec = "scripts/dev/build";
  };

  tasks."dev:test" = {
    description = "Run the Go and Swift test suites";
    exec = "scripts/dev/test";
  };

  tasks."dev:stage" = {
    description = "Stage a signed app bundle under dist/staged without launching it";
    exec = "scripts/dev/stage";
  };

  tasks."dev:inspect" = {
    description = "Inspect and verify staged artifacts without launching them";
    exec = "scripts/dev/inspect";
  };
}
