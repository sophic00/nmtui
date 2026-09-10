# Maintainer: Vaibhav Sijaria <139199971+sophic00@users.noreply.github.com>

pkgname=nmt
pkgver=$(<"$startdir/VERSION")
pkgrel=1
pkgdesc="Terminal UI for managing Wi-Fi with NetworkManager"
arch=('x86_64' 'aarch64')
url="https://github.com/sophic00/nmtui"
license=('MIT')
depends=('networkmanager')
makedepends=('git' 'go')
source=("$pkgname::git+$url.git#tag=v$pkgver")
sha256sums=('SKIP')

build() {
  cd "$pkgname"
  export CGO_ENABLED=0
  export GOFLAGS='-buildmode=pie -mod=readonly -trimpath'
  export GOTOOLCHAIN=local
  go build -ldflags "-X main.version=$pkgver" -o "$pkgname" .
}

check() {
  cd "$pkgname"
  export CGO_ENABLED=0
  export GOFLAGS='-mod=readonly'
  export GOTOOLCHAIN=local
  go test ./...
}

package() {
  cd "$pkgname"
  install -Dm755 "$pkgname" "$pkgdir/usr/bin/$pkgname"
  install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
  install -Dm644 docs/nmt.1 "$pkgdir/usr/share/man/man1/nmt.1"
}
