// PKG-A-1: sidebar excised — GtkAdmin hub (/admin/gtk) is the sole admin surface.
// Returning null (vs empty div) so .admin-menu styles don't reserve sidebar width on /admin/users, /admin/orders etc.
function MenuBar() {
  return null;
}

export default MenuBar;
