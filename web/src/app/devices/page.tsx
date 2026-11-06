import { redirect } from "next/navigation";
import { auth } from "@/auth";
import { listDevices } from "@/lib/device-auth";
import { revoke } from "./actions";
import { RevokeButton } from "./revoke-button";

const LOCAL = /\.local$/u;

const fmt = (d: Date) => d.toISOString().slice(0, 16).replace("T", " ");

export default async function DevicesPage() {
  const userId = (await auth())?.user?.id;
  if (!userId) {
    redirect("/");
  }
  const rows = await listDevices(userId);

  return (
    <main className="mx-auto flex min-h-svh max-w-4xl flex-col gap-6 p-6">
      <h1 className="text-2xl font-semibold">Devices</h1>
      {rows.length === 0 ? (
        <p className="text-muted-foreground">No devices are signed in.</p>
      ) : (
        <table className="w-full text-left text-sm">
          <thead className="text-muted-foreground">
            <tr>
              <th className="py-2">Name</th>
              <th>OS</th>
              <th>Version</th>
              <th>Last seen</th>
              <th>Added</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {rows.map((d) => (
              <tr className="border-t" key={String(d._id)}>
                <td className="py-2">{d.name.replace(LOCAL, "")}</td>
                <td>
                  {d.osName || d.os} / {d.arch}
                </td>
                <td>{d.version}</td>
                <td>{fmt(d.lastSeenAt)} UTC</td>
                <td>{fmt(d.createdAt)} UTC</td>
                <td className="text-right">
                  <form action={revoke}>
                    <input name="id" type="hidden" value={String(d._id)} />
                    <RevokeButton />
                  </form>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </main>
  );
}
