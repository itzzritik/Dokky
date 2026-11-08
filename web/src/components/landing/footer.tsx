import { DokkyMark } from "./brand";

export function Footer() {
  return (
    <footer className="border-t border-[#27272a] bg-black py-16">
      <div className="flex w-full flex-col items-center justify-between gap-6 px-6 sm:flex-row lg:px-16">
        <div className="flex items-center gap-3 text-[#a1a1aa]">
          <DokkyMark width={40} />
          <span className="text-[11px] font-bold tracking-widest uppercase">
            &copy; dokky {new Date().getFullYear()}
          </span>
        </div>
        <div className="flex items-center gap-10 text-[11px] tracking-widest text-[#a1a1aa] uppercase">
          <a
            className="transition-colors hover:text-[#22b8cc]"
            href="https://github.com/itzzritik/Dokky"
          >
            GitHub
          </a>
        </div>
      </div>
    </footer>
  );
}
