const platforms = {
  mac: { name: "macOS", arm: "Apple Silicon", x86: "Intel", defaultArm: true },
  windows: { name: "Windows", arm: "ARM64", x86: "x64" },
  linux: { name: "Linux", arm: "ARM64", x86: "x86_64" },
};

function detectOS() {
  const ua = navigator.userAgent;
  if (/Android|iPhone|iPad|iPod/.test(ua)) return null;
  const p = (navigator.userAgentData?.platform || navigator.platform || ua).toLowerCase();
  if (p.includes("mac")) return "mac";
  if (p.includes("win")) return "windows";
  if (p.includes("linux") || p.includes("x11")) return "linux";
  return null;
}

async function detectArm(os) {
  try {
    const { architecture } = await navigator.userAgentData.getHighEntropyValues(["architecture"]);
    if (architecture) return architecture === "arm";
  } catch {}
  if (/aarch64|arm64/i.test(navigator.userAgent)) return true;
  return !!platforms[os].defaultArm;
}

async function main() {
  const os = detectOS();
  const links = [...document.querySelectorAll("[data-asset]")];

  let release = null;
  try {
    const res = await fetch("https://api.github.com/repos/mudkipme/maibooru/releases/latest");
    if (res.ok) release = await res.json();
  } catch {}

  if (release) {
    for (const a of links) {
      const re = new RegExp(a.dataset.asset);
      const asset = release.assets.find((x) => re.test(x.name));
      if (asset) a.href = asset.browser_download_url;
    }
    const version = release.tag_name.replace(/^v/, "");
    const date = new Date(release.published_at).toLocaleDateString(undefined, { year: "numeric", month: "long", day: "numeric" });
    document.getElementById("release-line").textContent = `Version ${version}, released ${date}.`;
  }

  if (!os) return;
  const info = platforms[os];
  const arm = await detectArm(os);
  const card = document.querySelector(`[data-os="${os}"]`);
  card.classList.add("current");

  const primary = [...card.querySelectorAll("[data-asset]")].find((a) => /arm64|aarch64/.test(a.dataset.asset) === arm);
  const hero = document.getElementById("hero-download");
  hero.href = primary.href;
  document.getElementById("hero-label").textContent = `Download for ${info.name}`;
  const ext = primary.querySelector("span").textContent;
  const meta = [release && `v${release.tag_name.replace(/^v/, "")}`, `${arm ? info.arm : info.x86} ${ext}`].filter(Boolean).join(" · ");
  document.getElementById("hero-meta").textContent = meta;
}

main();
