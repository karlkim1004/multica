import { Linking, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { MulticaLogo } from "@/components/brand/multica-logo";

export default function Login() {
  return (
    <SafeAreaView className="flex-1 bg-background">
      <View className="flex-1 justify-center px-6 gap-6">
        <View className="items-center gap-3">
          <MulticaLogo size={32} />
          <View className="gap-1 items-center">
            <Text className="text-2xl font-semibold text-foreground">
              Sign in to Multica
            </Text>
            <Text className="text-sm text-muted-foreground text-center">
              Use the web app to sign in with Google.
            </Text>
          </View>
        </View>

        <Button size="lg" onPress={() => Linking.openURL("https://multica.nexai.co.kr")}>
          <Text>Open web app</Text>
        </Button>
      </View>
    </SafeAreaView>
  );
}
