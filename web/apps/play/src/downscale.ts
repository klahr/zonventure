/**
 * Shrink a camera photo before it goes up: a 48 MP shot over a weak
 * uplink is slow or refused, and the server keeps a small copy anyway.
 * Formats the server may not decode (HEIC) are re-encoded as JPEG when
 * the browser can read them. Anything unexpected sends the original.
 */
export async function downscalePhoto(file: File, maxDim = 1600, quality = 0.85): Promise<File> {
  if (!file.type.startsWith("image/") || file.type === "image/gif" || file.type === "image/svg+xml") return file;
  if (typeof createImageBitmap !== "function") return file;
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
  } catch {
    return file;
  }
  try {
    const longer = Math.max(bitmap.width, bitmap.height);
    const common = file.type === "image/jpeg" || file.type === "image/png" || file.type === "image/webp";
    if (longer <= maxDim && common) return file;
    const scale = Math.min(1, maxDim / longer);
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const ctx = canvas.getContext("2d");
    if (!ctx) return file;
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/jpeg", quality));
    if (!blob || (common && blob.size >= file.size)) return file;
    return new File([blob], (file.name || "photo").replace(/\.[^.]+$/, "") + ".jpg", { type: "image/jpeg", lastModified: Date.now() });
  } catch {
    return file;
  } finally {
    bitmap.close();
  }
}
