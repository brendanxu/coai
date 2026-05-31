import { CommonResponse, withNotify } from "@/api/common.ts";
import axios from "axios";
import { getErrorMessage } from "@/utils/base.ts";
import { getDeviceType, getDomain } from "@/payment/utils.ts";
import { appName } from "@/conf/env.ts";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

export type PaymentResponse = CommonResponse & {
  data?: {
    url: string;
    params: Record<string, string>;
  };
};

export type PaymentStatusResponse = CommonResponse & {
  order_state: boolean;
  remaining_time?: number;
};

export async function createPaymentOrder(
  type: string,
  quota: number,
  name: string,
): Promise<PaymentResponse> {
  try {
    const response = await axios.post<PaymentResponse>("/payment/create", {
      type,
      quota,
      domain: getDomain(),
      name: `${appName} - ${name}`,
      device: getDeviceType(),
    });
    return response.data;
  } catch (e) {
    return { status: false, error: getErrorMessage(e) };
  }
}

export async function getPaymentOrderStatus(
  order: string,
): Promise<PaymentStatusResponse> {
  try {
    const response = await axios.get<PaymentStatusResponse>(
      `/payment/check/${order}`,
    );
    return response.data;
  } catch (e) {
    return { status: false, error: getErrorMessage(e), order_state: false };
  }
}

export function usePaymentState(order: string): boolean {
  const { t } = useTranslation();
  const [state, setState] = useState(false);

  useEffect(() => {
    const interval = setInterval(async () => {
      const response = await getPaymentOrderStatus(order);
      withNotify(t, response);
      if (response.status && response.order_state) {
        setState(true);
        clearInterval(interval);
      }
    }, 2000);

    return () => clearInterval(interval);
  }, []);

  return state;
}
