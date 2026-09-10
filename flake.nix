{
  description = "nmt: a terminal UI for managing Wi-Fi with NetworkManager";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      version = builtins.head (builtins.match "([^\n]*)\n?" (builtins.readFile ./VERSION));

      nmtFor =
        pkgs: withChecks:
        pkgs.buildGo127Module {
          pname = "nmt";
          inherit version;
          src = self;

          vendorHash = "sha256-s0Tg4J8PCKIoJ8oA5/QDGRYRfOJ8dVcqLTuMSlDj958=";

          ldflags = [
            "-s"
            "-w"
            "-X"
            "main.version=${version}"
          ];

          # buildGoModule names the output after the module (nmtui), so
          # rename it to the installed command.
          postInstall = ''
            mv $out/bin/nmtui $out/bin/nmt
          '';

          doCheck = withChecks;

          meta = with pkgs.lib; {
            description = "Terminal UI for managing Wi-Fi with NetworkManager";
            license = licenses.mit;
            mainProgram = "nmt";
            platforms = platforms.linux;
          };
        };
    in
    {
      packages = forAllSystems (
        pkgs:
        rec {
          nmt = nmtFor pkgs false;
          default = nmt;
        }
      );

      checks = forAllSystems (
        pkgs:
        rec {
          nmt = nmtFor pkgs true;
          default = nmt;
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
