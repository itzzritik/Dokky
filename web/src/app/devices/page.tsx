import { redirect } from "next/navigation";
import { auth } from "@/auth";
import { listDevices } from "@/lib/device-auth";
import { services } from "@/lib/sync";
import { remove } from "./actions";
import { RemoveDevice } from "./remove-device";

const LOCAL = /\.local$/u;

const fmt = (d: Date) => d.toISOString().slice(0, 16).replace("T", " ");

const ONLINE_MS = 16 * 60_000;
const IDLE_MS = 90 * 24 * 60 * 60_000;

export default async function DevicesPage({
  searchParams,
}: {
  searchParams: Promise<{ erase?: string }>;
}) {
  const userId = (await auth())?.user?.id;
  if (!userId) {
    redirect("/");
  }
  const { erase } = await searchParams;
  const [rows, owned] = await Promise.all([
    listDevices(userId),
    services()
      .find({ userId }, { projection: { name: 1 } })
      .toArray(),
  ]);
  const accounts = owned.map((s) => s.name).sort();
  const now = Date.now();

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
                <td className="py-2">
                  {d.name.replace(LOCAL, "")}
                  {now - d.lastSeenAt.getTime() > IDLE_MS && (
                    <span className="text-muted-foreground ml-2 text-xs">
                      Not seen in 3 months
                    </span>
                  )}
                </td>
                <td>
                  {d.osName || d.os} / {d.arch}
                </td>
                <td>{d.version}</td>
                <td>{fmt(d.lastSeenAt)} UTC</td>
                <td>{fmt(d.createdAt)} UTC</td>
                <td className="text-right">
                  <form action={remove}>
                    <input name="id" type="hidden" value={String(d._id)} />
                    <RemoveDevice
                      accounts={accounts}
                      erase={erase === String(d._id)}
                      name={d.name.replace(LOCAL, "")}
                      offline={now - d.lastSeenAt.getTime() > ONLINE_MS}
                    />
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
