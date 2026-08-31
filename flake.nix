{
  description = "nmtui: a terminal UI for managing Wi-Fi with NetworkManager";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      nmtuiFor =
        pkgs: withChecks:
        pkgs.buildGo127Module {
          pname = "nmtui";
          version = "0.1.0";
          src = self;

          vendorHash = "sha256-s0Tg4J8PCKIoJ8oA5/QDGRYRfOJ8dVcqLTuMSlDj958=";

          doCheck = withChecks;

          meta = with pkgs.lib; {
            description = "Terminal UI for managing Wi-Fi with NetworkManager";
            license = licenses.mit;
            mainProgram = "nmtui";
            platforms = platforms.linux;
          };
        };
    in
    {
      packages = forAllSystems (
        pkgs:
        rec {
          nmtui = nmtuiFor pkgs false;
          default = nmtui;
        }
      );

      checks = forAllSystems (
        pkgs:
        rec {
          nmtui = nmtuiFor pkgs true;
          default = nmtui;
        }
      );

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go_1_27
            gopls
            gnumake
          ];
        };
      });
    };
}
