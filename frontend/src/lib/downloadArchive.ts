import JSZip from "jszip";

export interface DownloadArchiveOutput {
  download_url: string;
  fileName: string;
  outputFormat: string;
  variant_name: string;
}

export async function downloadOutputsArchive(
  outputs: DownloadArchiveOutput[],
  archiveName = buildArchiveName(),
) {
  if (outputs.length === 0) return;

  const zip = new JSZip();
  const usedPaths = new Map<string, number>();

  await Promise.all(
    outputs.map(async (output) => {
      const response = await fetch(output.download_url, { mode: "cors" });
      if (!response.ok) {
        throw new Error(`failed to fetch ${output.variant_name} (${response.status})`);
      }

      const blob = await response.blob();
      zip.file(nextArchivePath(output, usedPaths), blob);
    }),
  );

  const archiveBlob = await zip.generateAsync({
    type: "blob",
    compression: "DEFLATE",
    compressionOptions: { level: 6 },
  });

  triggerBlobDownload(archiveBlob, archiveName);
}

function buildArchiveName() {
  const stamp = new Date().toISOString().replace(/[:]/g, "-").replace(/\.\d{3}Z$/, "Z");
  return `photon-outputs-${stamp}.zip`;
}

function nextArchivePath(
  output: DownloadArchiveOutput,
  usedPaths: Map<string, number>,
) {
  const fileStem = sanitizePathSegment(stripExtension(output.fileName)) || "image";
  const variant = sanitizePathSegment(output.variant_name) || "output";
  const extension = sanitizePathSegment(output.outputFormat) || "bin";
  const basePath = `${fileStem}/${variant}.${extension}`;
  const seen = usedPaths.get(basePath) ?? 0;
  usedPaths.set(basePath, seen + 1);

  if (seen === 0) {
    return basePath;
  }

  return `${fileStem}/${variant}-${seen + 1}.${extension}`;
}

function stripExtension(fileName: string) {
  return fileName.replace(/\.[^.]+$/, "");
}

function sanitizePathSegment(input: string) {
  const cleaned = input
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");

  return cleaned;
}

function triggerBlobDownload(blob: Blob, fileName: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = fileName;
  anchor.rel = "noopener";
  anchor.style.display = "none";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();

  window.setTimeout(() => {
    URL.revokeObjectURL(url);
  }, 1000);
}
