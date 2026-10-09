/** 本文件负责展示 GoodHR 品牌标识。 */
import "@/components/public-site.css";

/** BrandMark 输出可点击的 GoodHR 品牌标识。 */
export default function BrandMark() {
  return (
    <a href="/" className="public-brandmark-1">
      <img
        src="/brand/goodhr-logo-transparent-512.png"
        alt=""
        className="public-brandmark-2"
      />
      <p className="public-text public-brandmark-3">GoodHR</p>
    </a>
  );
}
