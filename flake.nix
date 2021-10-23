{
  description = "Get some REM sleep knowing your files are safe";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }: flake-utils.lib.eachDefaultSystem (system: let
    pkgs = nixpkgs.legacyPackages.${system};
  in rec {
    packages.rem = pkgs.buildGoModule {
      name = "rem";
      src = ./.;
      vendorSha256 = "sha256-cPxyrEy4i1Fj3pji7eYHHorlmmb+SuM7Y+mmP5OpdUc";
      meta = with pkgs.lib; {
        description = "Get some REM sleep knowing your files are safe";
        homepage = "https://github.com/quackduck/rem";
        license = licenses.mit;
        platforms = platforms.linux ++ platforms.darwin;
      };
    };
    defaultPackage = packages.rem;
  });
}
