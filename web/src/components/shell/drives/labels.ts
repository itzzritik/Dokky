const TYPES: Record<string, string> = {
  azureblob: "Azure Blob Storage",
  azurefiles: "Azure Files",
  b2: "Backblaze B2",
  box: "Box",
  drive: "Google Drive",
  dropbox: "Dropbox",
  ftp: "FTP",
  "google cloud storage": "Google Cloud Storage",
  "google photos": "Google Photos",
  iclouddrive: "iCloud Drive",
  koofr: "Koofr",
  mediafire: "MediaFire",
  mega: "MEGA",
  onedrive: "OneDrive",
  pcloud: "pCloud",
  protondrive: "Proton Drive",
  s3: "S3 storage",
  sftp: "SSH / SFTP",
  smb: "Network share",
  webdav: "WebDAV",
  yandex: "Yandex Disk",
};

export const typeLabel = (type: string) => TYPES[type] ?? type;

// Drives are often named after their account, so only a label that adds something is shown.
export const via = (account: { name: string; type: string }, drive: string) =>
  [account.name, typeLabel(account.type)].find(
    (label) => label.toLowerCase() !== drive.toLowerCase()
  ) ?? "";
