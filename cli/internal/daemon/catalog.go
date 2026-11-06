package daemon

import (
	"cmp"
	"slices"
	"strings"

	"github.com/itzzritik/dokky/cli/internal/api"
	"github.com/itzzritik/dokky/cli/internal/engine"
)

// most used first; Popular ones also sit on the welcome screen
var ranked = []api.Provider{
	{Type: "drive", Name: "Google Drive", Desc: "Google cloud storage, including shared drives.", Group: "cloud", Popular: true},
	{Type: "onedrive", Name: "OneDrive", Desc: "Microsoft cloud storage for personal and work accounts.", Group: "cloud", Popular: true},
	{Type: "dropbox", Name: "Dropbox", Desc: "Dropbox cloud storage for personal and team accounts.", Group: "cloud", Popular: true},
	{Type: "iclouddrive", Name: "iCloud Drive", Desc: "Apple cloud storage, signed in with your Apple Account."},
	{Type: "google photos", Name: "Google Photos", Desc: "Your Google Photos library, with albums as folders."},
	{Type: "s3", Name: "S3 storage", Desc: "Amazon S3 and compatible storage like R2, Wasabi and MinIO.", Group: "own", Popular: true},
	{Type: "mega", Name: "MEGA", Desc: "End to end encrypted cloud storage from MEGA.", Group: "cloud", Popular: true},
	{Type: "box", Name: "Box", Desc: "Box cloud storage for personal and business accounts.", Group: "cloud", Popular: true},
	{Type: "protondrive", Name: "Proton Drive", Desc: "End to end encrypted cloud storage from Proton.", Group: "cloud", Popular: true},
	{Type: "pcloud", Name: "pCloud", Desc: "Swiss cloud storage from pCloud.", Group: "cloud", Popular: true},
	{Type: "mediafire", Name: "MediaFire", Desc: "Cloud storage and file sharing from MediaFire.", Group: "cloud"},
	{Type: "sftp", Name: "SSH / SFTP", Desc: "Files on any server reachable over SSH, such as a VPS or NAS.", Group: "own", Popular: true},
	{Type: "smb", Name: "Network share", Desc: "SMB shares on a NAS, Windows PC or file server.", Group: "own", Popular: true},
	{Type: "webdav", Name: "Nextcloud", Desc: "Nextcloud, ownCloud and any other WebDAV server.", Group: "own", Popular: true},
	{Type: "b2", Name: "Backblaze B2", Desc: "Object storage buckets on Backblaze B2."},
	{Type: "google cloud storage", Name: "Google Cloud Storage", Desc: "Object storage buckets on Google Cloud, not Google Drive."},
	{Type: "azureblob", Name: "Azure Blob Storage", Desc: "Object storage containers on Microsoft Azure."},
	{Type: "ftp", Name: "FTP", Desc: "Files on any FTP or FTPS server."},
	{Type: "yandex", Name: "Yandex Disk", Desc: "Cloud storage from Yandex."},
	{Type: "mailru", Name: "Mail.ru Cloud", Desc: "Cloud storage from Mail.ru."},
	{Type: "koofr", Name: "Koofr", Desc: "European cloud storage from Koofr, including Digi Storage."},
	{Type: "jottacloud", Name: "Jottacloud", Desc: "Norwegian cloud storage from Jottacloud and its partners."},
	{Type: "storj", Name: "Storj", Desc: "Decentralized, encrypted object storage from Storj."},
	{Type: "seafile", Name: "Seafile", Desc: "Seafile file sync servers, self-hosted or hosted."},
	{Type: "azurefiles", Name: "Azure Files", Desc: "Managed SMB file shares on Microsoft Azure."},
	{Type: "filen", Name: "Filen", Desc: "End to end encrypted cloud storage from Filen."},
	{Type: "internxt", Name: "Internxt Drive", Desc: "End to end encrypted cloud storage from Internxt."},
	{Type: "zoho", Name: "Zoho WorkDrive", Desc: "Team file storage in Zoho WorkDrive."},
	{Type: "hidrive", Name: "HiDrive", Desc: "Cloud storage from STRATO HiDrive."},
	{Type: "sharefile", Name: "Citrix ShareFile", Desc: "Secure business file sharing from Citrix ShareFile."},
	{Type: "swift", Name: "OpenStack Swift", Desc: "OpenStack Swift object storage, such as OVH and Rackspace."},
	{Type: "oracleobjectstorage", Name: "Oracle Object Storage", Desc: "Object storage buckets on Oracle Cloud Infrastructure."},
	{Type: "putio", Name: "Put.io", Desc: "Cloud downloads and storage from Put.io."},
	{Type: "pikpak", Name: "PikPak", Desc: "Cloud storage from PikPak."},
	{Type: "premiumizeme", Name: "premiumize.me", Desc: "Cloud downloads and storage from premiumize.me."},
	{Type: "fichier", Name: "1Fichier", Desc: "File hosting and storage from 1Fichier."},
	{Type: "gofile", Name: "Gofile", Desc: "File hosting and sharing from Gofile."},
	{Type: "pixeldrain", Name: "Pixeldrain", Desc: "File hosting and sharing from Pixeldrain."},
	{Type: "opendrive", Name: "OpenDrive", Desc: "Cloud storage and backup from OpenDrive."},
	{Type: "huaweidrive", Name: "Huawei Drive", Desc: "Huawei cloud storage for Huawei ID accounts."},
	{Type: "sugarsync", Name: "SugarSync", Desc: "Cloud storage and sync from SugarSync."},
	{Type: "internetarchive", Name: "Internet Archive", Desc: "Items and collections on the Internet Archive."},
	{Type: "filescom", Name: "Files.com", Desc: "Managed file transfer and storage from Files.com."},
	{Type: "filefabric", Name: "Enterprise File Fabric", Desc: "Enterprise File Fabric from Storage Made Easy."},
	{Type: "quatrix", Name: "Quatrix", Desc: "Secure file sharing from Quatrix by Maytech."},
	{Type: "linkbox", Name: "Linkbox", Desc: "Cloud storage from Linkbox."},
	{Type: "ulozto", Name: "Uloz.to", Desc: "File hosting and storage from Uloz.to."},
	{Type: "filelu", Name: "FileLu", Desc: "Cloud storage and file hosting from FileLu."},
	{Type: "qingstor", Name: "QingStor", Desc: "Object storage from QingCloud."},
	{Type: "netstorage", Name: "Akamai NetStorage", Desc: "Origin storage on Akamai NetStorage."},
	{Type: "sia", Name: "Sia", Desc: "Decentralized cloud storage on the Sia network."},
	{Type: "hdfs", Name: "HDFS", Desc: "Hadoop Distributed File System clusters."},
	{Type: "http", Name: "Web folder", Desc: "Files from a web server's directory listing, read only."},
	{Type: "cloudinary", Name: "Cloudinary", Desc: "Media assets in your Cloudinary library."},
	{Type: "imagekit", Name: "ImageKit", Desc: "Media assets in your ImageKit library."},
	{Type: "drime", Name: "Drime", Desc: "Cloud storage from Drime."},
	{Type: "shade", Name: "Shade", Desc: "Cloud storage from Shade."},
	{Type: "doi", Name: "DOI datasets", Desc: "Research datasets from DOI links, such as Zenodo, read only."},
}

// wrappers over other remotes, and local disks, make no sense as a drive
var notDrives = []string{"alias", "archive", "cache", "chunker", "combine", "compress", "crypt", "hasher", "local", "memory", "union"}

func catalog() ([]api.Provider, error) {
	ps, err := engine.Providers()
	if err != nil {
		return nil, err
	}
	out := make([]api.Provider, 0, len(ps))
	for _, p := range ranked {
		if slices.ContainsFunc(ps, func(x engine.Provider) bool { return x.Type == p.Type }) {
			out = append(out, p)
		}
	}
	// a service a newer rclone adds goes last until it's ranked
	var rest []api.Provider
	for _, p := range ps {
		if slices.Contains(notDrives, p.Type) || slices.ContainsFunc(ranked, func(x api.Provider) bool { return x.Type == p.Type }) {
			continue
		}
		name, hint, _ := strings.Cut(p.Description, " (")
		rest = append(rest, api.Provider{Type: p.Type, Name: name, Desc: cmp.Or(strings.TrimSuffix(hint, ")"), "Storage from "+name+".")})
	}
	slices.SortFunc(rest, func(a, b api.Provider) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return append(out, rest...), nil
}
