#!/usr/bin/env bash
# 构建可部署 Portal Shell、单例共享依赖与签名插件 ESM 入口。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
OUT_DIR="${PORTAL_OUT_DIR:-bin/portal}"
case "$OUT_DIR" in
  ""|"/"|".") echo "拒绝不安全的 Portal 输出目录: $OUT_DIR" >&2; exit 2 ;;
esac
rm -rf -- "${OUT_DIR:?}"
ASSETS="$OUT_DIR/assets"
VENDOR="$ASSETS/vendor"
mkdir -p "$VENDOR"

common=(--bundle --format=esm --platform=browser --target=es2022 --legal-comments=none --minify '--define:process.env.NODE_ENV="production"')
shared=(--external:react --external:react/jsx-runtime --external:react-dom --external:react-dom/client --external:@vastplan/portal-ui --external:@vastplan/ui-contract)

pnpm exec esbuild core/kernels/frontend/src/browser.tsx "${common[@]}" "${shared[@]}" --outfile="$ASSETS/portal-kernel.js"
pnpm exec esbuild core/kernels/frontend/src/vendor/react-stack.ts "${common[@]}" --outfile="$VENDOR/react-stack.js"
pnpm exec esbuild core/kernels/frontend/src/vendor/react.ts "${common[@]}" --external:./react-stack.js --outfile="$VENDOR/react.js"
pnpm exec esbuild core/kernels/frontend/src/vendor/react-jsx-runtime.ts "${common[@]}" --external:./react-stack.js --outfile="$VENDOR/react-jsx-runtime.js"
pnpm exec esbuild core/kernels/frontend/src/vendor/react-dom.ts "${common[@]}" --external:./react-stack.js --outfile="$VENDOR/react-dom.js"
node --no-warnings --input-type=module -e '
  import { pathToFileURL } from "node:url";
  const root = pathToFileURL(process.argv[1]).href;
  const react = await import(new URL("react.js", root));
  const runtime = await import(new URL("react-jsx-runtime.js", root));
  const dom = await import(new URL("react-dom.js", root));
  const missing = [
    [react, "useState"], [runtime, "Fragment"], [runtime, "jsx"],
    [runtime, "jsxs"], [dom, "createRoot"], [dom, "createPortal"],
  ].filter(([module, name]) => typeof module[name] === "undefined").map(([, name]) => name);
  if (missing.length > 0) {
    throw new Error(`React ESM 适配产物缺少导出: ${missing.join(", ")}`);
  }
' "$VENDOR/"
pnpm exec esbuild core/kernels/frontend/src/vendor/ui-contract.ts "${common[@]}" --outfile="$VENDOR/ui-contract.js"
pnpm exec esbuild core/kernels/frontend/src/vendor/portal-ui.ts "${common[@]}" --external:react --external:@vastplan/ui-contract --outfile="$VENDOR/portal-ui.js"

pnpm run build:frontend:plugins
cp core/kernels/frontend/static/index.html "$OUT_DIR/index.html"
cp core/kernels/frontend/static/portal.css "$ASSETS/portal.css"
echo "已构建 Portal 静态产物: $OUT_DIR"
