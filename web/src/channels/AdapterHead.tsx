import { PageHead } from "../PageHead";
import type { ChannelAdapter } from "../gen/silo/v1/ui_pb";
import { AdapterLogo } from "./AdapterLogo";

// The title row of a channel page: back button, the adapter's logo, a title and subtitle.
export function AdapterHead({ adapter, title, subtitle, onBack }: { adapter: ChannelAdapter; title: string; subtitle: string; onBack: () => void }) {
  return <PageHead mark={<AdapterLogo adapter={adapter} size={40} />} title={title} subtitle={subtitle} onBack={onBack} />;
}
