import { useTranslation } from "react-i18next";
import { useDispatch, useSelector } from "react-redux";
import {
  logout,
  selectAdmin,
  selectAuthenticated,
  selectUsername,
} from "@/store/auth.ts";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu.tsx";
import { Button } from "@/components/ui/button.tsx";
import router from "@/router.tsx";
import React from "react";
import {
  PackageCheck,
  Shield,
  Sparkles,
  User,
  Wallet,
} from "lucide-react";
import Icon from "@/components/utils/Icon.tsx";
import { HIDE_CREDIT_UI } from "@/conf/env.ts";

type MenuBarProps = {
  children: React.ReactNode;
  className?: string;
};

type MenuBarItemProps = {
  icon: React.ReactElement;
  path: string;
  name: string;
};

const BarItem = ({ icon, path, name }: MenuBarItemProps) => {
  const { t } = useTranslation();
  const navigate = () => router.navigate(path);

  return (
    <DropdownMenuItem onClick={navigate}>
      <Icon icon={icon} className={`w-4 h-4 mr-1.5`} />
      {t(`bar.${name}-full`)}
    </DropdownMenuItem>
  );
};

function MenuBar({ children, className }: MenuBarProps) {
  const { t } = useTranslation();
  const dispatch = useDispatch();
  const auth = useSelector(selectAuthenticated);
  const username = useSelector(selectUsername);
  const admin = useSelector(selectAdmin);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent className={className} align={`end`}>
        {auth ? (
          <>
            <DropdownMenuLabel className={`username`}>
              {username}
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            {/* v0.21 cleanup — chat (icon-as-/) + model marketplace entries removed.
                See PKG-CLEANUP. */}
            {!HIDE_CREDIT_UI && (
              <BarItem icon={<Wallet />} path={`/wallet`} name={"wallet"} />
            )}
            <BarItem icon={<Sparkles />} path={`/pricing`} name={"pricing"} />
            {/* PKG-N4 — /orders is auth-gated; this whole branch is already
                gated on `auth`, so no extra check needed here. */}
            <BarItem icon={<PackageCheck />} path={`/orders`} name={"orders"} />
            <BarItem icon={<User />} path={`/account`} name={"account"} />
            {/* <BarItem icon={<PieChart />} path={`/log`} name={"log"} /> */}
            {admin && (
              <BarItem icon={<Shield />} path={`/admin`} name={"admin"} />
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Button
                size={`sm`}
                className={`action-button`}
                onClick={() => dispatch(logout())}
              >
                {t("logout")}
              </Button>
            </DropdownMenuItem>
          </>
        ) : (
          <DropdownMenuItem asChild>
            <Button
              size={`sm`}
              className={`h-max w-full cursor-pointer`}
              onClick={() => router.navigate("/login")}
            >
              {t("login")}
            </Button>
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export default MenuBar;
