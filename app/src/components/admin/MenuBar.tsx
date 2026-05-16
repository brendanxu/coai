import { useSelector } from "react-redux";
import { selectMenu } from "@/store/menu.ts";
import { cn } from "@/components/ui/lib/utils.ts";


function MenuBar() {
  const open = useSelector(selectMenu);
  // PKG-A-1: sidebar excised — GtkAdmin hub is the sole admin surface.
  return <div className={cn("admin-menu", open && "open")} />;
}

export default MenuBar;
